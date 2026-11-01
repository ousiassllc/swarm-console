package updatecheck

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

// FirstDelay は起動から最初の確認までの待ち（起動処理・サンプラーの初回取得と重ねない。P-45）。
const FirstDelay = time.Minute

// Snapshot は Runner が持つ最後の結果（メモリだけ。再起動で失ってよい。FR-UPD-7）。
type Snapshot struct {
	Status    Status
	Current   string
	Latest    string // ValidTag を通った版だけ。Unknown のときは前回の成功の値が残る
	Reason    Reason // Status が Unknown のときだけ
	CheckedAt time.Time
}

// RunnerOptions は Runner の入力。
type RunnerOptions struct {
	Checker *Checker
	Current string
	// Disabled が true（update_check = false）なら Start は何もせず、Snapshot は Disabled を返す。
	Disabled bool
	Interval time.Duration // 確認の間隔（config の update_check_interval）
	// FirstDelay は 0 なら 1 分。
	FirstDelay time.Duration
	Clock      clock.Clock // nil なら clock.Real
	Logger     *slog.Logger
	// OnResult は確認のたびに、同じ goroutine から呼ばれる（update_available で自己更新に進むため。FR-UPD-7）。nil 可。
	OnResult func(ctx context.Context, r Result)
}

// Runner は serve の定期確認。確認と OnResult は 1 本の goroutine で行う（FR-UPD-8）。
type Runner struct {
	o RunnerOptions

	mu         sync.Mutex
	snap       Snapshot
	lastReason Reason // 前回の失敗の reason（成功で空に戻る）
	announced  string // 新しい版として知らせた版
}

// NewRunner は Runner を作る。
func NewRunner(o RunnerOptions) *Runner {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.FirstDelay <= 0 {
		o.FirstDelay = FirstDelay
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	r := &Runner{o: o, snap: Snapshot{Status: Pending, Current: o.Current}}
	if o.Disabled {
		r.snap.Status = Disabled
	}
	return r
}

// Snapshot は最後の結果。呼び出しで通信はしない（FR-UPD-9）。
func (r *Runner) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snap
}

// Start は確認の goroutine を 1 本起動し、終わると閉じる channel を返す。
// 最初の確認は FirstDelay 後、以後は Interval ごと。ctx が終わると通信を中断して止まる。
func (r *Runner) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	if r.o.Disabled {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		wait := r.o.FirstDelay
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.o.Clock.After(wait):
			}
			res := r.o.Checker.Check(ctx)
			if ctx.Err() != nil {
				return // 終了中の中断。結果は捨てる
			}
			r.record(res)
			if r.o.OnResult != nil {
				r.o.OnResult(ctx, res)
			}
			wait = r.o.Interval
		}
	}()
	return done
}

// record は結果を保持し、ログを出す（新しい版は初めて見たとき 1 回、失敗は reason が変わったときだけ。FR-UPD-8）。
func (r *Runner) record(res Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.snap
	r.snap = Snapshot{
		Status: res.Status, Current: r.o.Current, Latest: res.Latest, Reason: res.Reason, CheckedAt: r.o.Clock.Now(),
	}
	if res.Status == Unknown {
		r.snap.Latest = prev.Latest
		if res.Reason != r.lastReason {
			r.o.Logger.Warn("新しい版を確認できませんでした", "reason", string(res.Reason))
		}
		r.lastReason = res.Reason
		return
	}
	r.lastReason = ""
	if res.Status == UpdateAvailable && res.Latest != r.announced {
		r.announced = res.Latest
		r.o.Logger.Info("新しい版があります", "current", r.o.Current, "latest", res.Latest)
	}
}
