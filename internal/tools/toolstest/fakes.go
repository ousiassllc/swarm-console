// Package toolstest は tools・mcpapi・server のテストが使うフェイク（pane.Registry・pane.Ops・host.Host）と
// 一式の組み立て（Fixture）を提供する。テスト専用だが、複数パッケージのテストから使うため通常のファイルに置く。
package toolstest

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Registry は固定のスナップショットを返す pane.Registry。
type Registry struct {
	mu   sync.Mutex
	Snap *pane.Snapshot
	// Tails は ID ごとの画面の本文。無ければ DefaultTail。
	Tails       map[string]pane.Tail
	DefaultTail pane.Tail
	// Cards は ID ごとの質問カード。
	Cards map[string]*agent.Card

	// 呼び出しの記録。
	TailLines int
	TailRaw   bool
	TailCalls int
}

var _ pane.Registry = (*Registry)(nil)

// Current は固定のスナップショット。
func (r *Registry) Current(context.Context) (*pane.Snapshot, error) { return r.Snap, nil }

// Refresh は Current と同じ。
func (r *Registry) Refresh(context.Context) (*pane.Snapshot, error) { return r.Snap, nil }

// Resolve はスナップショットから探す。
func (r *Registry) Resolve(_ context.Context, arg string) (pane.Ref, error) {
	p, ok := r.Snap.Find(arg)
	if !ok {
		return pane.Ref{}, apperr.New(apperr.PaneNotFound, "pane", arg)
	}
	return pane.Ref{ID: p.ID, Pos: p.Pos}, nil
}

// Tail は Tails[ref.ID]（無ければ DefaultTail）を返し、引数を記録する。
func (r *Registry) Tail(_ context.Context, ref pane.Ref, lines int, raw bool) (pane.Tail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.TailLines, r.TailRaw = lines, raw
	r.TailCalls++
	if t, ok := r.Tails[ref.ID]; ok {
		return t, nil
	}
	return r.DefaultTail, nil
}

// Card は Cards[ref.ID]。エージェントなしのペインは unsupported_agent。
func (r *Registry) Card(_ context.Context, ref pane.Ref) (*agent.Card, error) {
	p, _ := r.Snap.Find(ref.ID)
	if p == nil || p.Agent == "" {
		return nil, apperr.New(apperr.UnsupportedAgent, "pane", ref.Pos)
	}
	return r.Cards[ref.ID], nil
}

// Ops は呼び出しを記録する pane.Ops。結果と失敗は Result*・Err で差し替える。
type Ops struct {
	mu    sync.Mutex
	Calls []string // 呼ばれた操作名の列

	Err error // 非 nil なら全操作がこのエラーを返す

	Pick        pane.PickResult
	Send        pane.SendResult
	EscapeRes   pane.EscapeResult
	StartRes    pane.StartResult
	CloseRes    pane.CloseResult
	DispatchRes pane.DispatchResult

	// 引数の記録。
	LastRef      pane.Ref
	LastN        int
	LastText     string
	LastAgent    string
	LastCwd      string
	LastTitle    string
	LastDispatch pane.DispatchArgs
}

var _ pane.Ops = (*Ops)(nil)

func (o *Ops) record(name string, ref pane.Ref) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Calls = append(o.Calls, name)
	o.LastRef = ref
	return o.Err
}

// Called は操作が 1 回でも呼ばれたか。
func (o *Ops) Called() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.Calls) > 0
}

// PickAsk は記録して Pick を返す。
func (o *Ops) PickAsk(_ context.Context, ref pane.Ref, n int, text string) (pane.PickResult, error) {
	o.LastN, o.LastText = n, text
	return o.Pick, o.record("PickAsk", ref)
}

// SendText は記録して Send を返す。
func (o *Ops) SendText(_ context.Context, ref pane.Ref, text string) (pane.SendResult, error) {
	o.LastText = text
	return o.Send, o.record("SendText", ref)
}

// Escape は記録して EscapeResを返す。
func (o *Ops) Escape(_ context.Context, ref pane.Ref) (pane.EscapeResult, error) {
	return o.EscapeRes, o.record("Escape", ref)
}

// Start は記録して StartResを返す。
func (o *Ops) Start(_ context.Context, ref pane.Ref, agentName, cwd, title string) (pane.StartResult, error) {
	o.LastAgent, o.LastCwd, o.LastTitle = agentName, cwd, title
	return o.StartRes, o.record("Start", ref)
}

// Close は記録して CloseResを返す。
func (o *Ops) Close(_ context.Context, ref pane.Ref) (pane.CloseResult, error) {
	return o.CloseRes, o.record("Close", ref)
}

// Dispatch は記録して DispatchResを返す。
func (o *Ops) Dispatch(_ context.Context, ref pane.Ref, a pane.DispatchArgs) (pane.DispatchResult, error) {
	o.LastDispatch = a
	return o.DispatchRes, o.record("Dispatch", ref)
}

// Host は固定値を返す host.Host。
type Host struct {
	St   host.Status
	Auth map[string]host.Auth
}

var _ host.Host = (*Host)(nil)

// Status は St。
func (h *Host) Status(context.Context, []string) (host.Status, error) { return h.St, nil }

// ProcTree は空。
func (h *Host) ProcTree(int) ([]host.Proc, error) { return nil, nil }

// AuthStatus は Auth[agent]（無ければ unknown）。
func (h *Host) AuthStatus(name, _ string, _ time.Time) host.Auth {
	if a, ok := h.Auth[name]; ok {
		return a
	}
	return host.Auth{Agent: name, Status: host.AuthUnknown}
}
