package reportvalues

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

// 起動から最初の取得までの待ち（report-values.md §2.1・§4。FR-UPD-7 の 1 分後の確認と重ねない）。
const (
	UsageFirstDelay = 10 * time.Second
	OmpFirstDelay   = 90 * time.Second
)

// Options は Collector の入力。
type Options struct {
	Runner Runner
	Clock  clock.Clock // nil なら clock.Real
	Logger *slog.Logger

	// UsageCheck が false なら使用状況は実行せず disabled（usage_check）。
	UsageCheck    bool
	UsageInterval time.Duration
	// OmpCheck が false なら omp の確認は実行せず disabled（update_check）。
	OmpCheck    bool
	OmpInterval time.Duration

	// FirstDelay は 0 なら既定（UsageFirstDelay・OmpFirstDelay）。
	UsageFirstDelay, OmpFirstDelay time.Duration
}

// Collector は決まった間隔で 2 つのコマンドを実行し、最後の結果を保持する。
// 結果は不変の値で、読み取りの呼び出しではコマンドを実行しない（FR-REP-10）。
type Collector struct {
	o Options

	mu    sync.Mutex
	usage Usage
	omp   Omp
}

// New は Collector を作る。起動直後は pending（止めたものは disabled）。
func New(o Options) *Collector {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.UsageFirstDelay <= 0 {
		o.UsageFirstDelay = UsageFirstDelay
	}
	if o.OmpFirstDelay <= 0 {
		o.OmpFirstDelay = OmpFirstDelay
	}
	c := &Collector{o: o, usage: pendingUsage(), omp: Omp{Status: OmpPending}}
	if !o.UsageCheck {
		c.usage.Status = StatusDisabled
	}
	if !o.OmpCheck {
		c.omp.Status = OmpDisabled
	}
	return c
}

func pendingUsage() Usage {
	return Usage{Status: StatusPending, Claude: []Account{}, Cursor: []CursorAccount{}, Codex: []Account{}}
}

// Usage は最後の使用状況。
func (c *Collector) Usage() Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usage
}

// Omp は最後の omp の更新確認。
func (c *Collector) Omp() Omp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.omp
}

// Start は有効なコマンドごとに goroutine を 1 本起動する。ctx が終わると実行を中断して止まり、返した関数が終了を待つ。
func (c *Collector) Start(ctx context.Context) (wait func()) {
	var wg sync.WaitGroup
	if c.o.UsageCheck {
		wg.Go(func() { c.loop(ctx, c.o.UsageFirstDelay, c.o.UsageInterval, c.collectUsage) })
	}
	if c.o.OmpCheck {
		wg.Go(func() { c.loop(ctx, c.o.OmpFirstDelay, c.o.OmpInterval, c.collectOmp) })
	}
	return wg.Wait
}

// loop は first 後に 1 回、以後は前の実行が終わってから interval ごとに run する。
func (c *Collector) loop(ctx context.Context, first, interval time.Duration, run func(context.Context)) {
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.o.Clock.After(wait):
		}
		run(ctx)
		wait = interval
	}
}

func (c *Collector) collectUsage(ctx context.Context) {
	out, reason := c.o.Runner.Run(ctx, CmdUsage)
	if ctx.Err() != nil {
		return // 終了中の中断。結果は捨てる
	}
	var u Usage
	if reason == "" {
		var err error
		if u, err = ParseUsage(out); err != nil {
			reason = ReasonParse
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason != "" {
		c.o.Logger.Warn("使用状況を取得できませんでした", "reason", reason)
		prev := c.usage
		if prev.Status == StatusOK || prev.Status == StatusStale {
			prev.Status, prev.Reason = StatusStale, reason // 前の成功分を残す
			c.usage = prev
			return
		}
		c.usage = Usage{Status: StatusUnavailable, Reason: reason, Claude: []Account{}, Cursor: []CursorAccount{}, Codex: []Account{}}
		return
	}
	if u.FetchedAt.IsZero() {
		u.FetchedAt = c.o.Clock.Now().UTC()
	}
	c.usage = u
}

func (c *Collector) collectOmp(ctx context.Context) {
	out, reason := c.o.Runner.Run(ctx, CmdUpdateCheck)
	if ctx.Err() != nil {
		return
	}
	var res Omp
	if reason == "" {
		var ok bool
		if res, ok = ParseOmp(out); !ok {
			reason = ReasonParse
		}
	}
	if reason != "" {
		c.o.Logger.Warn("omp の更新確認ができませんでした", "reason", reason)
		res = Omp{Status: OmpUnknown, Reason: reason}
	}
	res.CheckedAt = c.o.Clock.Now().UTC()
	c.mu.Lock()
	c.omp = res
	c.mu.Unlock()
}
