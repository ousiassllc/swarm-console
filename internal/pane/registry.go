package pane

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

// buildTimeout はスナップショット 1 回の上限（個々の tmux 呼び出しは tmux 側で 3 秒に制限される）。
const buildTimeout = 30 * time.Second

// flight は進行中（または次に始める）スナップショット取得 1 回分。
type flight struct {
	done chan struct{}
	snap *Snapshot
	err  error
}

func newFlight() *flight { return &flight{done: make(chan struct{})} }

// SnapshotRegistry は Registry の実装。サンプラー（Run）と読み取りツールから共有される。
type SnapshotRegistry struct {
	deps  Deps
	clk   clock.Clock
	log   *slog.Logger
	names []string // アダプタ名（昇順。特定の順序を決定的にする）

	mu      sync.Mutex
	cur     *Snapshot
	running *flight
	queued  *flight // running が終わったあとに始める強制再取得（同時に来た分をまとめる）

	// 以下は build（同時に 1 つしか走らない）だけが触る。
	hist       map[string]*paneHist
	known      map[string]seenPane       // 最後に見えたペイン（vanished のメタ用）
	absent     map[string]int            // 連続して見えなかった回数
	prevPos    []posEntry                // 前回の ID・位置（位置順）。nil は比較対象なし
	layoutWarn map[string]*layoutWarning // セッションごと
	versionLog bool
	built      bool      // 1 回でもスナップショットを作ったか
	startedAt  time.Time // 起動時刻（最初のスナップショットの changed_at）
}

var _ Registry = (*SnapshotRegistry)(nil)

// NewRegistry は Registry を作る。サンプラーを動かすには Run を呼ぶ。
func NewRegistry(deps Deps) (*SnapshotRegistry, error) {
	deps, err := deps.WithDefaults()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(deps.Adapters))
	for n, a := range deps.Adapters {
		if a != nil {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return &SnapshotRegistry{
		deps: deps, clk: deps.Clock, startedAt: deps.Clock.Now(), log: deps.Logger, names: names,
		hist: map[string]*paneHist{}, known: map[string]seenPane{}, absent: map[string]int{}, layoutWarn: map[string]*layoutWarning{},
	}, nil
}

// Deps は NewRegistry が既定値を埋めた依存（ops パッケージが共有する）。
func (r *SnapshotRegistry) Deps() Deps { return r.deps }

// Adapter は name の登録済みアダプタ。
func (r *SnapshotRegistry) Adapter(name string) (agent.Adapter, bool) {
	a, ok := r.deps.Adapters[name]
	return a, ok && a != nil
}

// Current は snapshot_ttl 以内ならキャッシュを返し、古ければ 1 回だけ取り直す（同時の呼び出しは同じ結果を受け取る）。
func (r *SnapshotRegistry) Current(ctx context.Context) (*Snapshot, error) {
	return r.fetch(ctx, false)
}

// Refresh は強制再取得。進行中の取得は呼び出し前に始まったものなので、その次の 1 回を使う（同時の Refresh は 1 回にまとめる）。
func (r *SnapshotRegistry) Refresh(ctx context.Context) (*Snapshot, error) { return r.fetch(ctx, true) }

func (r *SnapshotRegistry) fetch(ctx context.Context, force bool) (*Snapshot, error) {
	r.mu.Lock()
	if !force && r.cur != nil && r.clk.Now().Sub(r.cur.At) < r.deps.Cfg.SnapshotTTL {
		s := r.cur
		r.mu.Unlock()
		return s, nil
	}
	var f *flight
	switch {
	case r.running == nil:
		f = newFlight()
		r.running = f
		go r.run(f) //nolint:gosec // 取得は複数の呼び出しで共有するので、1 人の呼び出しの ctx には結び付けない（buildTimeout で上限）
	case force:
		if r.queued == nil {
			r.queued = newFlight()
		}
		f = r.queued
	default:
		f = r.running
	}
	r.mu.Unlock()
	select {
	case <-f.done:
		return f.snap, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// run は f の分のスナップショットを作って待っている呼び出しに配り、続きの強制再取得があれば引き続き走らせる。
func (r *SnapshotRegistry) run(f *flight) {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	snap, err := r.build(ctx)
	cancel()
	r.mu.Lock()
	if err == nil {
		r.cur = snap
	}
	f.snap, f.err = snap, err
	close(f.done)
	r.running = r.queued
	r.queued = nil
	next := r.running
	r.mu.Unlock()
	if next != nil {
		go r.run(next)
	}
}
