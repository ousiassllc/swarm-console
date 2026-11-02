package ops

import (
	"context"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// checkStartArgs は pane_start・dispatch（shell のとき）の引数を検査する。
// cwd の許可判定は呼び出し側（safety.CheckCWD）済みだが、シェルに渡す前の二重防御として ' と制御文字を拒否する。
func (o *Operator) checkStartArgs(agentName, cwd, title string) (agent.Adapter, error) {
	ad, ok := o.reg.Adapter(agentName)
	if !ok {
		return nil, apperr.New(apperr.InvalidArgs, "field", "agent")
	}
	if cwd == "" || hasControl(cwd) || containsRune(cwd, '\'') {
		return nil, apperr.New(apperr.InvalidArgs, "field", "cwd")
	}
	if title == "" || utf8.RuneCountInString(title) > config.MaxTitleRunes || hasControl(title) {
		return nil, apperr.New(apperr.InvalidArgs, "field", "title")
	}
	return ad, nil
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

// Start は shell のペインで cwd へ移動してエージェントを起動し、idle を待つ。
func (o *Operator) Start(ctx context.Context, ref pane.Ref, agentName, cwd, title string) (pane.StartResult, error) {
	ad, err := o.checkStartArgs(agentName, cwd, title)
	if err != nil {
		return pane.StartResult{}, err
	}
	unlock, p, err := o.begin(ctx, ref, false)
	if err != nil {
		return pane.StartResult{}, err
	}
	defer unlock()
	if err := allow(p, agent.StateShell); err != nil {
		return pane.StartResult{}, err
	}
	return o.startLocked(ctx, p, agentName, ad, cwd, title)
}

// startLocked は排他を取った状態で起動する。タイトル → cd → 起動コマンドの順に送り、start_timeout まで idle を待つ。
// 待ちの途中で asking（フォルダ信頼の確認など）になったら待たずに ok=false で返す。
func (o *Operator) startLocked(ctx context.Context, p pane.PaneState, name string, ad agent.Adapter, cwd, title string) (pane.StartResult, error) {
	full := o.deps.Cfg.TitlePrefix + title
	tm := o.deps.Tmux
	if err := tm.SetTitle(ctx, p.ID, full); err != nil {
		return pane.StartResult{}, pane.TmuxError(err)
	}
	for i, cmd := range []string{"cd '" + cwd + "'", ad.StartCommand()} {
		if err := tm.SendLiteral(ctx, p.ID, cmd); err != nil {
			return pane.StartResult{}, pane.TmuxError(err)
		}
		if i == 0 {
			// 最初の送信に成功した後は、クライアントの切断・キャンセルで途中にならないよう ctx から切り離して
			// 起動まで（start_timeout で上限）やりきる（コマンドを入力行に残さない。NFR-AVL-7）。
			ctx = context.WithoutCancel(ctx)
		}
		if err := o.sendKeys(ctx, p.ID, "Enter"); err != nil {
			return pane.StartResult{}, err
		}
	}
	last, asked := agent.StateUnknown, false
	idle, err := o.waitFor(ctx, o.deps.Cfg.StartTimeout, refreshStep, func(ctx context.Context) bool {
		snap, err := o.reg.Refresh(ctx)
		if err != nil {
			return false
		}
		ps, ok := snap.Find(p.ID)
		if !ok {
			return false
		}
		last = ps.State
		if ps.State == agent.StateAsking {
			asked = true
			return true
		}
		return ps.State == agent.StateIdle && ps.Agent == name
	})
	if err != nil {
		return pane.StartResult{}, err
	}
	return pane.StartResult{OK: idle && !asked, State: last, Agent: name, Cwd: cwd, Title: full}, nil
}
