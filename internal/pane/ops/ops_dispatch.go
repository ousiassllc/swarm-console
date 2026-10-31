package ops

import (
	"context"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Dispatch は shell なら起動してから、idle なら そのまま、指示文を貼り付けて Enter を送る（FR-O6）。
// メモの保存は呼び出し側（Accepted に関わらず送信後に行う）。
func (o *Operator) Dispatch(ctx context.Context, ref pane.Ref, a pane.DispatchArgs) (pane.DispatchResult, error) {
	if err := o.checkBody(a.Text); err != nil {
		return pane.DispatchResult{}, err
	}
	ad, ok := o.reg.Adapter(a.Agent)
	if !ok {
		return pane.DispatchResult{}, apperr.New(apperr.InvalidArgs, "field", "agent")
	}
	unlock, p, err := o.begin(ctx, ref, true)
	if err != nil {
		return pane.DispatchResult{}, err
	}
	defer unlock()
	if err := allow(p, agent.StateShell, agent.StateIdle); err != nil {
		return pane.DispatchResult{}, err
	}
	res := pane.DispatchResult{}
	switch p.State {
	case agent.StateIdle:
		if p.Agent != a.Agent {
			return res, apperr.New(apperr.InvalidArgs, "field", "agent")
		}
	case agent.StateShell:
		if _, err := o.checkStartArgs(a.Agent, a.Cwd, a.Title); err != nil {
			return res, err
		}
		st, err := o.startLocked(ctx, p, a.Agent, ad, a.Cwd, a.Title)
		if err != nil {
			return res, err
		}
		if !st.OK {
			res.State = st.State
			return res, nil
		}
		res.Started = true
	}
	// 古い最終報告は送る前に archive/ へ移す（P-11）。失敗しても指示は送る。
	if _, err := pane.ArchiveReport(o.deps.FS, o.deps.Cfg.ReportDir, p.Pos, o.deps.Clock.Now()); err != nil {
		o.deps.Logger.Warn("古い最終報告を archive へ移せませんでした", "pane", p.ID, "err", err)
	}
	accepted, err := o.pasteAndAccept(ctx, p.ID, ad, a.Text, agent.StateIdle)
	if err != nil {
		return res, err
	}
	res.Accepted, res.OK = accepted, accepted
	res.State = o.stateOf(ctx, p.ID)
	return res, nil
}
