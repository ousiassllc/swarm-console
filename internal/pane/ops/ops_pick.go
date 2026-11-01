package ops

import (
	"context"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// pickAttempts はカーソル合わせの最大試行回数（最初の 1 回＋再試行 2 回。FR-ASK）。
const pickAttempts = 3

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
	card, err := o.readCardFresh(ctx, p.ID, ad)
	if err != nil {
		return pane.PickResult{}, err
	}
	target, picked, err := pickTarget(card, n, text)
	if err != nil {
		return pane.PickResult{}, err
	}
	if err := o.reachCursor(ctx, p.ID, ad, *card, target, text != ""); err != nil {
		return pane.PickResult{}, err
	}
	if text != "" {
		if err := o.submitCustom(ctx, p.ID, ad, text); err != nil {
			return pane.PickResult{}, err
		}
	} else if err := o.sendKeys(ctx, p.ID, "Enter"); err != nil {
		return pane.PickResult{}, err
	}
	closed, err := o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		st, _ := o.look(ctx, p.ID, ad)
		return st != agent.StateAsking && st != agent.StateUnknown
	})
	if err != nil {
		return pane.PickResult{}, err
	}
	return pane.PickResult{OK: closed, State: o.stateOf(ctx, p.ID), Picked: picked, CardClosed: closed}, nil
}

// readCardFresh は画面を取り直して質問カードを読む。カードが無ければ not_asking。
func (o *Operator) readCardFresh(ctx context.Context, id string, ad agent.Adapter) (*agent.Card, error) {
	text, err := o.deps.Tmux.Capture(ctx, id, pane.CaptureLines)
	if err != nil {
		return nil, pane.TmuxError(err)
	}
	c := pane.RawCard(ad, pane.SplitLines(text))
	if c == nil {
		return nil, apperr.New(apperr.NotAsking)
	}
	return c, nil
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
			names := make([]string, len(keys))
			for i, k := range keys {
				names[i] = k.Name
			}
			if err := o.sendKeys(ctx, id, names...); err != nil {
				return err
			}
		}
		text, err := o.deps.Tmux.Capture(ctx, id, pane.CaptureLines)
		if err != nil {
			return pane.TmuxError(err)
		}
		c, ok := ad.ParseCard(agent.NewScreen(pane.SplitLines(text)))
		if ok && c.Cursor == target {
			return nil
		}
		if ok {
			card = c
		}
	}
	return apperr.New(apperr.CursorMismatch, "target", target)
}

// submitCustom は「その他」を選んだ状態で text を貼り付けて Enter を送る。
// Enter のあとも質問のままでカードが読めない（自由入力欄が別に開いた）ときは、もう 1 度だけ貼り付けて Enter を送る。
func (o *Operator) submitCustom(ctx context.Context, id string, ad agent.Adapter, text string) error {
	enter := func() error {
		if err := o.deps.Tmux.Paste(ctx, id, text); err != nil {
			return pane.TmuxError(err)
		}
		return o.sendKeys(ctx, id, "Enter")
	}
	if err := enter(); err != nil {
		return err
	}
	inputOpen := false
	if _, err := o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		st, lines := o.look(ctx, id, ad)
		if st != agent.StateAsking {
			return true
		}
		_, ok := ad.ParseCard(agent.NewScreen(lines))
		inputOpen = !ok
		return inputOpen
	}); err != nil {
		return err
	}
	if inputOpen {
		return enter()
	}
	return nil
}
