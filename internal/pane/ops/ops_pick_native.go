package ops

import (
	"context"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// nativeSubmitSettle は tern の画面のペインで、選んだ Enter のあと、Submit の Enter を送るまでの待ち。
// tern の画面にはカーソルが Submit に移ったことが見えないので、omp が描き直す時間を固定で待つ。
const nativeSubmitSettle = 500 * time.Millisecond

// pickNative は tern の画面（TSP）のペインの質問カードで n 番を選ぶ（Issue #121）。
// 組み立てた画面にはカーソルも選んだ印も無い（カーソルは omp が最初に合わせる位置）ので、普通の画面のように、カーソルが合ったこと・
// チェックが付いたこと・Submit に移ったことを画面で確かめられない。次の条件のときだけ、確かめずにキーを送る。
//   - 質問が 1 つのフォーム（選んだあとに別の質問が開くと、Submit の Enter が次の質問の選択になってしまう）。
//   - 番号で選ぶ（「その他」への自由入力は、入力欄が開いたことを確かめられない）。
//
// 送るキーは、カーソルを n に動かす Up/Down → （複数選択なら Space）→ Enter → Submit の Enter。回答が記録されて質問でなくなったことで閉じたと見る。
// 人がカーソルを動かしたあとのカードには合わない（組み立てた画面は最初の位置のまま）。
func (o *Operator) pickNative(ctx context.Context, p pane.PaneState, ad agent.Adapter, card agent.Card, rd pane.Reading, n int, text string) (pane.PickResult, error) {
	if text != "" {
		return pane.PickResult{}, apperr.New(apperr.CustomNotAllowed, "reason", "native")
	}
	if rd.AskQuestions != 1 {
		return pane.PickResult{}, apperr.New(apperr.CursorMismatch, "target", n, "reason", "native_form", "questions", rd.AskQuestions)
	}
	target, picked, err := pickTarget(&card, n, "")
	if err != nil {
		return pane.PickResult{}, err
	}
	keys, err := ad.PickKeys(card, target)
	if err != nil {
		return pane.PickResult{}, apperr.As(err)
	}
	if tg, ok := ad.(agent.Toggler); ok {
		keys = append(keys, tg.ToggleKeys(agent.NewScreen(rd.Lines), n)...)
	}
	keys = append(keys, agent.Key{Name: "Enter"})
	if err := o.sendKeys(ctx, p.ID, keyNames(keys)...); err != nil {
		return pane.PickResult{}, err
	}
	if err := o.sleep(ctx, nativeSubmitSettle); err != nil {
		return pane.PickResult{}, err
	}
	if st, _ := o.look(ctx, p.ID, ad); st == agent.StateAsking {
		if err := o.sendKeys(ctx, p.ID, "Enter"); err != nil {
			return pane.PickResult{}, err
		}
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
