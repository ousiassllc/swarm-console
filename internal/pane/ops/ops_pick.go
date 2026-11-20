package ops

import (
	"context"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// pickAttempts はカーソル合わせの最大試行回数（最初の 1 回＋再試行 2 回。FR-ASK）。
const pickAttempts = 3

// カーソルを動かすキーを送ったあと、画面に反映されるのを待つ上限と、画面を取り直す間隔。
// omp は入力のあと描き直すまでに少し間があり、直後に読むと動く前のカーソルが読める。
const (
	cursorSettle = time.Second
	cursorStep   = 50 * time.Millisecond
)

// PickAsk は質問カードの n 番を選ぶ（text なら「その他」に text を入れる）。カーソルが合わないまま Enter は送らない。
func (o *Operator) PickAsk(ctx context.Context, ref pane.Ref, n int, text string) (pane.PickResult, error) {
	if (n == 0) == (text == "") {
		return pane.PickResult{}, apperr.New(apperr.InvalidArgs, "field", "n|text")
	}
	if text != "" {
		if err := o.checkText(text); err != nil {
			return pane.PickResult{}, err
		}
	}
	unlock, p, err := o.begin(ctx, ref, true)
	if err != nil {
		return pane.PickResult{}, err
	}
	defer unlock()
	if p.State != agent.StateAsking {
		return pane.PickResult{}, apperr.New(apperr.NotAsking, "pane", p.Pos, "state", string(p.State))
	}
	ad, err := o.adapterFor(p)
	if err != nil {
		return pane.PickResult{}, err
	}
	card, rd, err := o.readCardFresh(ctx, p.ID, ad)
	if err != nil {
		return pane.PickResult{}, err
	}
	if rd.Native {
		return o.pickNative(ctx, p, ad, *card, rd, n, text)
	}
	target, picked, err := pickTarget(card, n, text)
	if err != nil {
		return pane.PickResult{}, err
	}
	if err := o.reachCursor(ctx, p.ID, ad, *card, target, text != ""); err != nil {
		return pane.PickResult{}, err
	}
	if text == "" {
		if err := o.checkOption(ctx, p.ID, ad, n); err != nil {
			return pane.PickResult{}, err
		}
	}
	if text != "" {
		if err := o.submitCustom(ctx, p.ID, ad, text, card.Question); err != nil {
			return pane.PickResult{}, err
		}
	} else if err := o.sendKeys(ctx, p.ID, "Enter"); err != nil {
		return pane.PickResult{}, err
	}
	closed, err := o.awaitCardClosed(ctx, p.ID, ad, card.Question)
	if err != nil {
		return pane.PickResult{}, err
	}
	return pane.PickResult{OK: closed, State: o.stateOf(ctx, p.ID), Picked: picked, CardClosed: closed}, nil
}

// checkOption は複数選択の質問（agent.Toggler）で、カーソルがある n 番にチェックを付ける（Space）。付いたのを画面で確かめる。
// 単一選択・チェック済みでは何も送らない。付かなければ cursor_mismatch（Enter は送らない）。
func (o *Operator) checkOption(ctx context.Context, id string, ad agent.Adapter, n int) error {
	tg, ok := ad.(agent.Toggler)
	if !ok {
		return nil
	}
	toggle := func(ctx context.Context) ([]agent.Key, bool) {
		text, err := o.deps.Tmux.Capture(ctx, id, pane.CaptureLines)
		if err != nil {
			return nil, false
		}
		return tg.ToggleKeys(agent.NewScreen(pane.SplitLines(text)), n), true
	}
	if keys, ok := toggle(ctx); !ok {
		return apperr.New(apperr.TmuxError)
	} else if len(keys) == 0 {
		return nil
	} else if err := o.sendKeys(ctx, id, keyNames(keys)...); err != nil {
		return err
	}
	checked, err := o.waitFor(ctx, cursorSettle, cursorStep, func(ctx context.Context) bool {
		keys, ok := toggle(ctx)
		return ok && len(keys) == 0
	})
	if err != nil {
		return err
	}
	if !checked {
		return apperr.New(apperr.CursorMismatch, "target", n, "reason", "not_checked")
	}
	return nil
}

func keyNames(keys []agent.Key) []string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.Name
	}
	return names
}

// awaitCardClosed は、選んだ Enter のあとにカードが閉じるまで待つ（上限 accept_timeout）。閉じたら true。
//
// カードが閉じないまま、次の 2 つのどちらかになったら待ちを区切る。
//   - カーソルが Submit の行に移った（agent.Submitter。omp v19）。回答はまだ確定していないので、Enter を 1 回だけ送って確定する。
//   - 別の質問が開いた（質問が複数のフォーム）。選んだ分は済んでいて、次の質問は ask_card で読み直して選ぶ。閉じてはいない（false）。
func (o *Operator) awaitCardClosed(ctx context.Context, id string, ad agent.Adapter, question string) (bool, error) {
	sub, _ := ad.(agent.Submitter)
	submitted := false
	nextQuestion := false
	var sendErr error
	closed, err := o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		st, rd := o.look(ctx, id, ad)
		if st != agent.StateAsking {
			return st != agent.StateUnknown
		}
		screen := agent.NewScreen(rd.Lines)
		if sub != nil && !submitted && sub.SubmitFocused(screen) {
			submitted = true
			sendErr = o.sendKeys(ctx, id, "Enter")
			return sendErr != nil
		}
		if c, ok := ad.ParseCard(screen); ok && c.Question != question {
			nextQuestion = true
			return true
		}
		return false
	})
	switch {
	case sendErr != nil:
		return false, sendErr
	case err != nil:
		return false, err
	}
	return closed && !nextQuestion, nil
}

// readCardFresh は画面を取り直して質問カードを読む。カードが無ければ not_asking。
func (o *Operator) readCardFresh(ctx context.Context, id string, ad agent.Adapter) (*agent.Card, pane.Reading, error) {
	rd, err := o.reg.ReadScreen(ctx, id, pane.CaptureLines, ad)
	if err != nil {
		return nil, pane.Reading{}, err
	}
	c := pane.RawCard(ad, rd.Lines)
	if c == nil {
		return nil, pane.Reading{}, apperr.New(apperr.NotAsking)
	}
	return c, rd, nil
}

// pickTarget は目標のカーソル番号と、選んだ選択肢（text のときは nil）を決める。
// 「その他」は Options に含まれず、カーソルは len(Options)+1 になる（agent.ParseCard の約束）。
func pickTarget(c *agent.Card, n int, text string) (target int, picked *agent.Option, err error) {
	if text != "" {
		if !c.AllowsCustom {
			return 0, nil, apperr.New(apperr.CustomNotAllowed)
		}
		return len(c.Options) + 1, nil, nil
	}
	if n < 1 || n > len(c.Options) {
		return 0, nil, apperr.New(apperr.OptionOutOfRange, "n", n, "options", len(c.Options))
	}
	opt := c.Options[n-1]
	return n, &opt, nil
}

// reachCursor はキー列を送ってカーソルが target に合うまで確かめる（最大 pickAttempts 回）。合わなければ cursor_mismatch。
// キーを送った直後の画面はまだ描き直されていないことがあるので、合うまで cursorSettle の間だけ画面を取り直して待つ。
// 待たずに読み取って「動いていない」と決めつけ、同じ差分のキーを重ねて送ると、カーソルが行き過ぎて合わなくなる（Issue #20）。
func (o *Operator) reachCursor(ctx context.Context, id string, ad agent.Adapter, card agent.Card, target int, custom bool) error {
	for range pickAttempts {
		var keys []agent.Key
		var err error
		if custom {
			keys, err = ad.CustomKeys(card)
		} else {
			keys, err = ad.PickKeys(card, target)
		}
		if err != nil {
			return apperr.As(err)
		}
		if len(keys) > 0 {
			if err := o.sendKeys(ctx, id, keyNames(keys)...); err != nil {
				return err
			}
		}
		var captureErr error
		matched, err := o.waitFor(ctx, cursorSettle, cursorStep, func(ctx context.Context) bool {
			text, err := o.deps.Tmux.Capture(ctx, id, pane.CaptureLines)
			if err != nil {
				captureErr = err
				return true
			}
			c, ok := ad.ParseCard(agent.NewScreen(pane.SplitLines(text)))
			if !ok {
				return false
			}
			card = c // 合わなければ、次の試行はいま読めたカーソルからの差分で送る
			return c.Cursor == target
		})
		if captureErr != nil {
			return pane.TmuxError(captureErr)
		}
		if err != nil {
			return err
		}
		if matched {
			return nil
		}
	}
	return apperr.New(apperr.CursorMismatch, "target", target)
}

// submitCustom は「その他」を選んだ状態で text を貼り付けて Enter を送る。
// Enter のあとも質問のままでカードが読めない（自由入力欄が別に開いた）ときは、もう 1 度だけ貼り付けて Enter を送る。
// カーソルが Submit の行に移った（omp v19。入力はその行に入っている）、または別の質問が開いたときは、入力欄は開いていない。
func (o *Operator) submitCustom(ctx context.Context, id string, ad agent.Adapter, text, question string) error {
	enter := func() error {
		if err := o.deps.Tmux.Paste(ctx, id, text); err != nil {
			return pane.TmuxError(err)
		}
		return o.sendKeys(ctx, id, "Enter")
	}
	if err := enter(); err != nil {
		return err
	}
	sub, _ := ad.(agent.Submitter)
	inputOpen := false
	if _, err := o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		st, rd := o.look(ctx, id, ad)
		if st != agent.StateAsking {
			return true
		}
		screen := agent.NewScreen(rd.Lines)
		if sub != nil && sub.SubmitFocused(screen) {
			return true
		}
		c, ok := ad.ParseCard(screen)
		inputOpen = !ok
		return inputOpen || c.Question != question
	}); err != nil {
		return err
	}
	if inputOpen {
		return enter()
	}
	return nil
}
