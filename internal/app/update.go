package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/selfupdate"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// Option は New の任意の設定。
type Option func(*options)

type options struct {
	trial *selfupdate.Trial
	env   UpdateEnv
}

// WithTrial は起動の最初の試運転の判定（selfupdate.Probation）で試運転中になったときの Trial を渡す。
// apply は probation になり、待ち受けから 60 秒生きれば確定する（FR-UPD-17）。
func WithTrial(t *selfupdate.Trial) Option { return func(o *options) { o.trial = t } }

// WithUpdateEnv は新しい版の確認と自己更新の外部との境（ホーム・実行ファイル・通信路・時計など）を差し替える。
// テストが実際の GitHub・実際の ~/bin に触れずに serve を確かめるために使う。
func WithUpdateEnv(e UpdateEnv) Option { return func(o *options) { o.env = e } }

// UpdateEnv は確認と自己更新の差し替え口。ゼロ値は本番の値（実際のホーム・os.Executable・標準の通信路・実時計）。
type UpdateEnv struct {
	Home       string
	Executable func() (string, error)
	GOOS       string
	GOARCH     string
	// Transport は確認とダウンロードの通信路（nil なら標準）。
	Transport http.RoundTripper
	// Clock は確認の待ちと試運転の 60 秒に使う（nil なら実時計）。
	Clock clock.Clock
	// FirstDelay は最初の確認までの待ち（0 なら 1 分）。
	FirstDelay time.Duration
}

// updateRuntime は serve の確認（Runner）と自己更新（Updater）をつなぎ、host_status.update の元になる。
type updateRuntime struct {
	cfg     *config.Config
	runner  *updatecheck.Runner
	updater *selfupdate.Updater
	trial   *selfupdate.Trial
	clk     clock.Clock
	logger  *slog.Logger

	exitMu sync.Mutex
	exit   context.CancelFunc // Run が置く。入れ替えのあとの正常終了の要求（SIGTERM と同じ経路）

	// applyMu は確認の goroutine と試運転の確定の goroutine が、入れ替えと deferred・applied を同時に触らないようにする。
	applyMu  sync.Mutex
	deferred string // 試運転中に見送った update_available の最新版。確定したらすぐ再試行する（FR-UPD-12）
	applied  bool   // 入れ替えが済んだ（終了を要求した）。以後は入れ替えない
}

var _ tools.UpdateSource = (*updateRuntime)(nil)

func newUpdateRuntime(cfg *config.Config, version string, logger *slog.Logger, o options) (*updateRuntime, error) {
	home := o.env.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ホームフォルダを決められません: %w", err)
		}
		home = h
	}
	clk := o.env.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	u := &updateRuntime{cfg: cfg, trial: o.trial, clk: clk, logger: logger}
	updater, err := selfupdate.New(selfupdate.Options{
		Home: home, Current: version, Executable: o.env.Executable, GOOS: o.env.GOOS, GOARCH: o.env.GOARCH,
		Transport: o.env.Transport, Clock: clk, Logger: logger, RequestExit: u.requestExit,
	}, o.trial)
	if err != nil {
		return nil, err
	}
	u.updater = updater
	u.runner = updatecheck.NewRunner(updatecheck.RunnerOptions{
		Checker:  updatecheck.New(updatecheck.Options{Current: version, Transport: o.env.Transport}),
		Current:  version,
		Disabled: !cfg.UpdateCheck, Interval: cfg.UpdateCheckInterval, FirstDelay: o.env.FirstDelay,
		Clock: clk, Logger: logger, OnResult: u.onResult,
	})
	return u, nil
}

// requestExit は入れ替えのあとの正常終了を始める（SIGTERM と同じグレースフル停止。終了コード 0。FR-UPD-16）。
func (u *updateRuntime) requestExit() {
	u.exitMu.Lock()
	defer u.exitMu.Unlock()
	if u.exit != nil {
		u.exit()
	}
}

func (u *updateRuntime) setExit(cancel context.CancelFunc) {
	u.exitMu.Lock()
	defer u.exitMu.Unlock()
	u.exit = cancel
}

// onResult は確認のたびに、同じ goroutine から呼ばれる。update_available で update_auto なら自己更新に進む（FR-UPD-7）。
func (u *updateRuntime) onResult(ctx context.Context, r updatecheck.Result) {
	switch r.Status {
	case updatecheck.UpToDate:
		u.applyMu.Lock()
		defer u.applyMu.Unlock()
		u.deferred = ""
		u.updater.Reset()
	case updatecheck.UpdateAvailable:
		if !u.cfg.UpdateAuto {
			return
		}
		u.applyMu.Lock()
		defer u.applyMu.Unlock()
		u.applyLocked(ctx, r.Latest)
	case updatecheck.Unknown, updatecheck.Pending, updatecheck.Disabled:
	}
}

// applyLocked は latest へ入れ替える（applyMu を持って呼ぶ）。
// 失敗・中断は Updater が apply と警告に反映済み（reason だけ。FR-UPD-18）。次の周期まで再試行しない。
// ただし試運転中に見送った分は latest を覚え、試運転の確定で再試行する（FR-UPD-12）。
func (u *updateRuntime) applyLocked(ctx context.Context, latest string) {
	if u.applied {
		return
	}
	u.deferred = ""
	err := u.updater.Apply(ctx, latest)
	switch {
	case err == nil:
		u.applied = true
	case errors.Is(err, selfupdate.ErrProbation):
		u.deferred = latest
	case !isExpected(err):
		u.logger.Warn("自己更新を行えませんでした")
	}
}

// confirmTrial は試運転の確定を Updater へ伝え、試運転中に見送った入れ替えがあれば次の周期を待たずに再試行する。
// 確定と見送りの記録は applyMu の下で行うので、確定の直前に見送られた分を取りこぼさない。
func (u *updateRuntime) confirmTrial(ctx context.Context) {
	u.applyMu.Lock()
	defer u.applyMu.Unlock()
	u.updater.Confirmed()
	if latest := u.deferred; latest != "" {
		u.applyLocked(ctx, latest)
	}
}

// isExpected は Apply が状況へ反映済みで、追加の警告が要らない結果かを返す。
func isExpected(err error) bool {
	var f *selfupdate.Failure
	return errors.As(err, &f) || errors.Is(err, selfupdate.ErrBlocked) || errors.Is(err, selfupdate.ErrNotNewer) ||
		errors.Is(err, selfupdate.ErrProbation) || errors.Is(err, context.Canceled)
}

// start は確認の goroutine（1 本）と、試運転の確定の待ちを動かす。ctx が終わると通信を中断して止まり、返した関数が両方の終了を待つ。
func (u *updateRuntime) start(ctx context.Context, ready <-chan struct{}) (wait func()) {
	var wg sync.WaitGroup
	checkDone := u.runner.Start(ctx)
	wg.Go(func() { <-checkDone })
	if u.trial != nil {
		wg.Go(func() {
			if u.trial.Run(ctx, u.clk, ready) {
				u.confirmTrial(ctx)
			}
		})
	}
	return wg.Wait
}

// UpdateInfo は host_status.update の元（API 仕様 §5.3）。メモリの値を返すだけで、通信しない（FR-UPD-9）。
func (u *updateRuntime) UpdateInfo() tools.UpdateInfo {
	s, a := u.runner.Snapshot(), u.updater.Status()
	state := string(a.State)
	// off は「自己更新しない」設定。ただし試運転中（入れ替えがすでに起きている）は、設定によらず probation を見せる。
	if (!u.cfg.UpdateCheck || !u.cfg.UpdateAuto) && a.State != selfupdate.StateProbation {
		state = "off"
	}
	return tools.UpdateInfo{
		Status: string(s.Status), Current: s.Current, Latest: s.Latest, Reason: string(s.Reason), CheckedAt: s.CheckedAt,
		Apply: tools.ApplyInfo{State: state, Reason: string(a.Reason), At: a.At, From: a.From, Version: a.Version},
	}
}
