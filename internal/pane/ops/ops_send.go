package ops

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// inputAreaLines は入力欄とみなす画面の下側の非空行数（受理確認用）。
const inputAreaLines = 6

// enterRetryStep は Enter を送ってから、効かなかったと見て Enter を送り直すまでの間隔（Issue #101）。
const enterRetryStep = 2 * time.Second

// escapeLog は send_escape が自動で残すログ（FR-NOTE-10）。
const escapeLog = "Esc を送った（中断）"

// builtinCheckText は pane.Deps.CheckText が無いときの簡易検査。許可する制御文字は \n・\t だけ（P-13）。
func builtinCheckText(text string) error {
	if utf8.RuneCountInString(text) > config.MaxTextRunes {
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
	_, accepted, err := o.pasteAndAccept(ctx, p.ID, ad, text, p.State)
	if err != nil {
		return pane.SendResult{}, err
	}
	return pane.SendResult{OK: accepted, State: o.stateOf(ctx, p.ID), Accepted: accepted, Chars: utf8.RuneCountInString(text)}, nil
}

// pasteAndAccept は Paste → 1 秒 → （貼り付けの確認メニューが開いていれば閉じる）→ Enter → 受理待ち。prior は送る前の状態。
// 受理 = working になった。すでに working・stalled だったペインは、貼った本文の末尾が画面から消えたことも条件にする。
// Enter が効かないことがある（起動直後の omp は入力を受けても送信を受け付けず、負荷が高いと長引く。Issue #101）。
// Enter から enterRetryStep たっても画面が idle と読めるとき（working になっていない = 送信されていない）は Enter を送り直す。
// 画面が読めない（unknown）ときや working・stalled のままのときは送り直さない（作業中のエージェントに余計な Enter を送らない）。
// 送り直した Enter が空の入力欄に届いても何も起きない。受理されなければ ok:false のまま。
// メニューを閉じる待ちと受理待ちは 1 つの期限（固定の 1 秒のあと accept_timeout）を分け合う（NFR-PERF-4: 決着は 1 秒 + accept_timeout 以内）。
// 貼り付けに成功した後は、クライアントの切断・キャンセルで途中にならないよう ctx から切り離して Enter まで送る
// （入力欄に半端な本文を残さない。NFR-AVL-7）。sent は貼り付けに成功したか。
func (o *Operator) pasteAndAccept(ctx context.Context, id string, ad agent.Adapter, text string, prior agent.State) (sent, accepted bool, err error) {
	if err := o.deps.Tmux.Paste(ctx, id, text); err != nil {
		return false, false, pane.TmuxError(err)
	}
	ctx = context.WithoutCancel(ctx)
	if err := o.sleep(ctx, fixedWait); err != nil {
		return true, false, err
	}
	deadline := o.deps.Clock.Now().Add(o.deps.Cfg.AcceptTimeout)
	if closed, err := o.closePasteMenu(ctx, id, ad, deadline); err != nil || !closed {
		return true, false, err
	}
	lastEnter := o.deps.Clock.Now()
	if err := o.sendKeys(ctx, id, "Enter"); err != nil {
		return true, false, err
	}
	wasWorking := prior == agent.StateWorking || prior == agent.StateStalled
	var retryErr error
	accepted, err = o.waitFor(ctx, max(deadline.Sub(o.deps.Clock.Now()), 0), pollStep, func(ctx context.Context) bool {
		st, lines := o.look(ctx, id, ad)
		if st == agent.StateWorking && (!wasWorking || !onScreen(lines, text)) {
			return true
		}
		if now := o.deps.Clock.Now(); st == agent.StateIdle && now.Sub(lastEnter) >= enterRetryStep {
			lastEnter = now
			retryErr = o.sendKeys(ctx, id, "Enter")
			return retryErr != nil
		}
		return false
	})
	if retryErr != nil {
		return true, false, retryErr
	}
	return true, accepted, err
}

// closePasteMenu は、貼り付けの確認メニューが開いていれば閉じるキーを 1 回送り、閉じるまで deadline まで画面を取り直して待つ。
// 開いたまま Enter を送ると、メニューの選択になって依頼が送信されない（Issue #99）。
// メニューがなかった・閉じたら true。deadline までに閉じなければ false（Enter は送らない）。
// 閉じるキーはメニューが開いていると読めたときだけ送る（作業中のエージェントを Esc で中断しないため）。
// 画面が取れなかったときは開閉を判定できないので、メニューなしとも閉じたとも見なさない
// （最初の確認なら何も送らず false、待ちの途中なら取れるまで待つ）。
func (o *Operator) closePasteMenu(ctx context.Context, id string, ad agent.Adapter, deadline time.Time) (bool, error) {
	pm, ok := ad.(agent.PasteMenu)
	if !ok {
		return true, nil
	}
	// open は (メニューが開いているか, 画面が読めたか)。
	open := func(ctx context.Context) (isOpen, known bool) {
		text, err := o.deps.Tmux.Capture(ctx, id, pane.CaptureLines)
		if err != nil {
			return false, false
		}
		return pm.PasteMenuOpen(agent.NewScreen(pane.SplitLines(text))), true
	}
	isOpen, known := open(ctx)
	if !known {
		return false, nil
	}
	if !isOpen {
		return true, nil
	}
	if err := o.sendKeys(ctx, id, pm.PasteMenuClose().Name); err != nil {
		return false, err
	}
	return o.waitFor(ctx, max(deadline.Sub(o.deps.Clock.Now()), 0), pollStep, func(ctx context.Context) bool {
		isOpen, known := open(ctx)
		return known && !isOpen
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
