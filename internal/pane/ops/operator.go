// Package ops は pane.Ops の実装。ペインごとの排他・操作の送信・送信後の確認を担う（機能要件 §4.2）。
package ops

import (
	"context"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

const (
	pollStep    = 200 * time.Millisecond // 画面確認のポーリング間隔
	refreshStep = 500 * time.Millisecond // スナップショットを取り直して確認するときの間隔
	fixedWait   = time.Second            // 仕様で定めた固定の待ち（貼り付け→Enter、pane_close の各手順）
)

// Operator は pane.Ops の実装。Registry と同じ依存を使い、ペインごとの排他を持つ。
type Operator struct {
	reg   *pane.SnapshotRegistry
	deps  pane.Deps
	locks *lockMap
}

var _ pane.Ops = (*Operator)(nil)

// New は reg の依存を使う Operator を作る。
func New(reg *pane.SnapshotRegistry) *Operator {
	return &Operator{reg: reg, deps: reg.Deps(), locks: newLockMap()}
}

// Lock は id のペインの排他を取る（lock_timeout で apperr.BusyTimeout）。返り値で解放する。
func (o *Operator) Lock(ctx context.Context, id string) (func(), error) {
	return o.locks.lock(ctx, o.deps.Clock, id, o.deps.Cfg.LockTimeout)
}

// begin は排他を取り、スナップショットを取り直して最新のペインの状態を返す。
// 失敗したときは unlock 済み。unknown（エージェントなし）は needAgent のとき unsupported_agent。
func (o *Operator) begin(ctx context.Context, ref pane.Ref, needAgent bool) (unlock func(), p pane.PaneState, err error) {
	unlock, err = o.Lock(ctx, ref.ID)
	if err != nil {
		return nil, p, err
	}
	snap, err := o.reg.Refresh(ctx)
	if err != nil {
		unlock()
		return nil, p, apperr.Wrap(apperr.Internal, err)
	}
	ps, ok := snap.Find(ref.ID)
	if !ok {
		unlock()
		return nil, p, apperr.New(apperr.PaneNotFound, "pane", ref.Pos)
	}
	if needAgent && ps.State == agent.StateUnknown {
		unlock()
		return nil, p, apperr.New(apperr.UnsupportedAgent, "pane", ps.Pos)
	}
	return unlock, *ps, nil
}

// allow は p の状態が allowed に含まれなければ invalid_state を返す。
func allow(p pane.PaneState, allowed ...agent.State) error {
	for _, s := range allowed {
		if p.State == s {
			return nil
		}
	}
	return apperr.New(apperr.InvalidState, "pane", p.Pos, "state", string(p.State))
}

// stateOf はスナップショットを取り直した現在の状態。ペインが消えていれば unknown。
func (o *Operator) stateOf(ctx context.Context, id string) agent.State {
	snap, err := o.reg.Refresh(ctx)
	if err != nil {
		return agent.StateUnknown
	}
	if p, ok := snap.Find(id); ok {
		return p.State
	}
	return agent.StateUnknown
}

// sleep は clock 経由の待ち。ctx が終わったらそのエラーを返す。
func (o *Operator) sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-o.deps.Clock.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitFor は check が true を返すまで、上限 timeout まで step 間隔で繰り返す（上限付きの条件待ち）。
// 上限に達したら最後にもう 1 回確認する。条件を満たしたら true。
func (o *Operator) waitFor(ctx context.Context, timeout, step time.Duration, check func(context.Context) bool) (bool, error) {
	// 期限は時計の時刻で持つ。待ち用タイマーを期限のために置くと、check が実時間で終わるのを待つ間も
	// 「待機中のタイマーがある」と数えられ、テストの偽の時計が先に進んでしまう。待つのは step の間だけにする。
	deadline := o.deps.Clock.Now().Add(timeout)
	for {
		if check(ctx) {
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !o.deps.Clock.Now().Before(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-o.deps.Clock.After(step):
		}
	}
}

// look は画面を取り直し、アダプタで状態を読む。読めなければ (Unknown, nil)。
func (o *Operator) look(ctx context.Context, id string, ad agent.Adapter) (agent.State, []string) {
	text, err := o.deps.Tmux.Capture(ctx, id, pane.CaptureLines)
	if err != nil {
		return agent.StateUnknown, nil
	}
	lines := pane.SplitLines(text)
	return ad.Detect(agent.NewScreen(lines)).State, lines
}

// adapterFor は p のエージェントのアダプタ。
func (o *Operator) adapterFor(p pane.PaneState) (agent.Adapter, error) {
	ad, ok := o.reg.Adapter(p.Agent)
	if !ok {
		return nil, apperr.New(apperr.UnsupportedAgent, "pane", p.Pos)
	}
	return ad, nil
}

// sendKeys は tmux のキー名を 1 回の send-keys で送る。
func (o *Operator) sendKeys(ctx context.Context, id string, keys ...string) error {
	if err := o.deps.Tmux.SendKeys(ctx, id, keys...); err != nil {
		return pane.TmuxError(err)
	}
	return nil
}
