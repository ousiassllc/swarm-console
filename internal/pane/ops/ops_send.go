package ops

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// maxTextRunes は貼り付け本文の上限（FR-O2）。
const maxTextRunes = 8000

// inputAreaLines は入力欄とみなす画面の下側の非空行数（受理確認用）。
const inputAreaLines = 6

// escapeLog は send_escape が自動で残すログ（FR-NOTE-10）。
const escapeLog = "Esc を送った（中断）"

// builtinCheckText は pane.Deps.CheckText が無いときの簡易検査。許可する制御文字は \n・\t だけ（P-13）。
func builtinCheckText(text string) error {
	if utf8.RuneCountInString(text) > maxTextRunes {
		return apperr.New(apperr.TextTooLong)
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return apperr.New(apperr.ForbiddenControlChar)
		}
	}
	return nil
}

func (o *Operator) checkText(text string) error {
	if o.deps.CheckText != nil {
		return o.deps.CheckText(text)
	}
	return builtinCheckText(text)
}

// checkBody は貼り付ける本文を検査する。空は invalid_args。
func (o *Operator) checkBody(text string) error {
	if strings.TrimSpace(text) == "" {
		return apperr.New(apperr.InvalidArgs, "field", "text")
	}
	return o.checkText(text)
}

// SendText は text を貼り付けて Enter を送る（P-09: idle・working・stalled のみ）。
func (o *Operator) SendText(ctx context.Context, ref pane.Ref, text string) (pane.SendResult, error) {
	if err := o.checkBody(text); err != nil {
		return pane.SendResult{}, err
	}
	unlock, p, err := o.begin(ctx, ref, true)
	if err != nil {
		return pane.SendResult{}, err
	}
	defer unlock()
	if err := allow(p, agent.StateIdle, agent.StateWorking, agent.StateStalled); err != nil {
		return pane.SendResult{}, err
	}
	ad, err := o.adapterFor(p)
	if err != nil {
		return pane.SendResult{}, err
	}
	accepted, err := o.pasteAndAccept(ctx, p.ID, ad, text, p.State)
	if err != nil {
		return pane.SendResult{}, err
	}
	return pane.SendResult{OK: accepted, State: o.stateOf(ctx, p.ID), Accepted: accepted, Chars: utf8.RuneCountInString(text)}, nil
}

// pasteAndAccept は Paste → 1 秒 → Enter → 受理待ち。prior は送る前の状態。
// 受理 = working になった。すでに working・stalled だったペインは、貼った本文の末尾が画面から消えたことも条件にする。
func (o *Operator) pasteAndAccept(ctx context.Context, id string, ad agent.Adapter, text string, prior agent.State) (bool, error) {
	if err := o.deps.Tmux.Paste(ctx, id, text); err != nil {
		return false, pane.TmuxError(err)
	}
	if err := o.sleep(ctx, fixedWait); err != nil {
		return false, err
	}
	if err := o.sendKeys(ctx, id, "Enter"); err != nil {
		return false, err
	}
	wasWorking := prior == agent.StateWorking || prior == agent.StateStalled
	return o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		st, lines := o.look(ctx, id, ad)
		return st == agent.StateWorking && (!wasWorking || !onScreen(lines, text))
	})
}

// onScreen は text の最後の非空行の末尾（30 字）が画面の下側（入力欄）にまだ残っているか。
func onScreen(lines []string, text string) bool {
	parts := strings.Split(strings.TrimRight(text, " \t\r\n"), "\n")
	tail := strings.TrimSpace(parts[len(parts)-1])
	if rs := []rune(tail); len(rs) > 30 {
		tail = string(rs[len(rs)-30:])
	}
	if tail == "" {
		return false
	}
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < inputAreaLines; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		if strings.Contains(lines[i], tail) {
			return true
		}
	}
	return false
}

// Escape は Esc を送って状態の遷移を待つ。許可は working・stalled・asking・idle。confirm の検査は tools 層。
func (o *Operator) Escape(ctx context.Context, ref pane.Ref) (pane.EscapeResult, error) {
	unlock, p, err := o.begin(ctx, ref, true)
	if err != nil {
		return pane.EscapeResult{}, err
	}
	defer unlock()
	if err := allow(p, agent.StateWorking, agent.StateStalled, agent.StateAsking, agent.StateIdle); err != nil {
		return pane.EscapeResult{}, err
	}
	ad, err := o.adapterFor(p)
	if err != nil {
		return pane.EscapeResult{}, err
	}
	before, _ := o.look(ctx, p.ID, ad)
	if err := o.sendKeys(ctx, p.ID, "Escape"); err != nil {
		return pane.EscapeResult{}, err
	}
	changed, werr := o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		st, _ := o.look(ctx, p.ID, ad)
		return st != agent.StateUnknown && st != before
	})
	// Esc は送ったので、待ちが途中で打ち切られても記録は残す。
	if _, err := o.deps.Notes.Log(context.WithoutCancel(ctx), p.ID, escapeLog); err != nil {
		o.deps.Logger.Warn("Esc のログを残せませんでした", "pane", p.ID, "err", err)
	}
	if werr != nil {
		return pane.EscapeResult{}, werr
	}
	active := before == agent.StateWorking || before == agent.StateAsking
	return pane.EscapeResult{OK: changed, State: o.stateOf(ctx, p.ID), Interrupted: changed && active}, nil
}
