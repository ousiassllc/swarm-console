package selfupdate

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// Options は Updater の入力。ホーム・実行ファイルのパス・通信路・時計・OS は差し替えられる（NFR-TEST-16）。
type Options struct {
	// Home は $HOME（置き場は Home/bin/swarm-console に固定。テストでは一時ディレクトリ）。
	Home string
	// Current は動いているバイナリの版。
	Current string
	// Executable は動いているバイナリのパス。nil なら os.Executable。
	Executable func() (string, error)
	// GOOS・GOARCH は空なら runtime の値。
	GOOS, GOARCH string
	// Transport はダウンロードの通信路。nil なら接続 10 秒の標準。テストが偽の配信元へ向ける。
	// リダイレクトの制限・サイズ上限・ヘッダーは Transport によらず Updater が決める。
	Transport http.RoundTripper
	// Timeout はダウンロード全体の上限。0 なら 5 分。
	Timeout time.Duration
	Clock   clock.Clock // nil なら clock.Real
	Logger  *slog.Logger
	// RequestExit は入れ替えのあと、SIGTERM と同じ正常終了（終了コード 0）を始める（FR-UPD-16）。必須。
	RequestExit func()

	// FS は入れ替えのファイル操作。nil なら実際のファイル操作（OSFS）。テストが各ステップの失敗を作るために差し替える。
	FS FS
}

// Updater は自己更新を行い、その状況（apply）を持つ。
type Updater struct {
	o      Options
	dir    string
	client *http.Client

	mu       sync.Mutex
	apply    Apply
	lastWarn string // 前回警告した失敗（reason か blocked:<版>）。変わったときだけ警告する
}

// New は Updater を作る。trial が non-nil なら apply は probation（from 付き）で始まる。
func New(o Options, trial *Trial) (*Updater, error) {
	if o.RequestExit == nil {
		return nil, errors.New("selfupdate: RequestExit が必要です")
	}
	if o.Home == "" {
		return nil, errors.New("selfupdate: Home が必要です")
	}
	if o.Executable == nil {
		o.Executable = os.Executable
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Timeout <= 0 {
		o.Timeout = totalTimeout
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.FS == nil {
		o.FS = OSFS{}
	}
	u := &Updater{o: o, dir: Dir(o.Home), client: newClient(o.Transport, o.Timeout), apply: Apply{State: StateIdle}}
	if trial != nil {
		u.apply = Apply{State: StateProbation, From: trial.From}
	}
	return u, nil
}

// Status は自己更新の状況（host_status.update.apply）。
func (u *Updater) Status() Apply {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.apply
}

// Confirmed は試運転が確定したときに呼ぶ（probation → idle）。
func (u *Updater) Confirmed() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.apply.State == StateProbation {
		u.apply = Apply{State: StateIdle}
	}
}

// Reset は、新しい版が無くなった（最新・確認できた）ときに、前の failed・blocked を idle へ戻す。probation は変えない。
func (u *Updater) Reset() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.apply.State == StateFailed || u.apply.State == StateBlocked {
		u.apply = Apply{State: StateIdle}
	}
}

// Apply は latest（updatecheck が update_available と判定した vX.Y.Z）へ入れ替える（FR-UPD-12〜16）。
// 成功すると RequestExit を呼んで nil を返す。入れ替えない・できないときは本体を旧バイナリのまま残して次を返す：
// *Failure（reason 付き。apply は failed）、ErrBlocked（apply は blocked）、ErrNotNewer（状況は変えない）、
// ErrInvalidVersion、ctx のエラー（SIGTERM での中断。一時ファイルは消してある）。
func (u *Updater) Apply(ctx context.Context, latest string) error {
	if !updatecheck.ValidTag(latest) {
		return ErrInvalidVersion
	}
	if err := u.precheck(latest); err != nil {
		u.record(latest, err)
		return err
	}
	u.o.Logger.Info("自己更新を始めます", "from", u.o.Current, "to", latest)
	dlCtx, cancel := context.WithTimeout(ctx, u.o.Timeout)
	defer cancel()
	if err := u.install(dlCtx, latest); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		u.record(latest, err)
		return err
	}
	u.o.RequestExit()
	return nil
}

// precheck は FR-UPD-12 の 4 条件（platform・置き場・新しい・戻した版より新しい）を、この順に確かめる。
func (u *Updater) precheck(latest string) error {
	if u.o.GOOS != "linux" || u.o.GOARCH != "amd64" {
		return fail(ReasonUnsupportedPlatform)
	}
	if !u.managed() {
		return fail(ReasonNotManaged)
	}
	if !updatecheck.Newer(latest, u.o.Current) {
		return ErrNotNewer
	}
	if v, ok := readFailed(u.dir); ok && !updatecheck.Newer(latest, v) {
		return &blockedError{version: v}
	}
	return nil
}

// managed は動いているバイナリが Home/bin/swarm-console/swarm-console の通常ファイル（リンクでない）で、
// そのフォルダに書けるかを返す。
func (u *Updater) managed() bool {
	exe, err := u.o.Executable()
	if err != nil || exe != filepath.Join(u.dir, binName) {
		return false
	}
	fi, err := os.Lstat(exe)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	return syscall.Access(u.dir, 2) == nil // W_OK
}

type blockedError struct{ version string }

func (e *blockedError) Error() string        { return ErrBlocked.Error() }
func (e *blockedError) Is(target error) bool { return target == ErrBlocked }

// record は失敗を apply に反映する。警告は前回と変わったときだけ（FR-UPD-18）。
func (u *Updater) record(latest string, err error) {
	var f *Failure
	var b *blockedError
	u.mu.Lock()
	defer u.mu.Unlock()
	var key string
	switch {
	case errors.As(err, &f):
		u.apply = Apply{State: StateFailed, Reason: f.Reason, At: u.o.Clock.Now()}
		key = string(f.Reason)
	case errors.As(err, &b):
		u.apply = Apply{State: StateBlocked, Version: b.version}
		key = "blocked:" + b.version
	default:
		return
	}
	if key != u.lastWarn {
		u.lastWarn = key
		u.o.Logger.Warn("自己更新できませんでした", "reason", key, "from", u.o.Current, "to", latest)
	}
}
