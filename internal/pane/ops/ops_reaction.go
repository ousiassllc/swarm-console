package ops

import (
	"context"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// awaitReaction は、作業中のペインに貼った text が会話の記録にどう現れるかを、since（貼る前の時刻）以降の記録で読み、受理のあとから reactionWindow まで待って返す（Issue #120）。
// 実行（acted）・無視（ignored）が決まったらそこで返し、決まらなければ最後の読み取り（unverified）を返す。
// アダプタが記録を読めない種類・pid が分からないときは、待たずに unverified（unsupported）。
// 画面は見ない（記録が主。tern の画面でも普通の画面でも同じ結果になる）。本文はログに出さない。
func (o *Operator) awaitReaction(ctx context.Context, p pane.PaneState, ad agent.Adapter, text string, since time.Time) (agent.Reaction, string, error) {
	rp, ok := ad.(agent.ReactionProbe)
	if !ok || p.AgentPID == 0 {
		return agent.ReactionUnverified, agent.BasisUnsupported, nil
	}
	last := agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisNotFound}
	waitStart := o.deps.Clock.Now()
	_, err := o.waitFor(ctx, o.reactionWindow(), refreshStep, func(context.Context) bool {
		last = rp.Reaction(p.AgentPID, since, text)
		return last.Final
	})
	if err != nil {
		return "", "", err
	}
	o.deps.Logger.Info("追加の指示への反応を確かめました", "pane", p.ID, "reaction", last.Reaction, "basis", last.Basis, "elapsed", o.deps.Clock.Now().Sub(waitStart))
	return last.Reaction, last.Basis, nil
}

// reactionWindow は、作業中のペインに送った指示への反応を待つ上限（受理を確かめたあとから）。
// 新しい設定は増やさず、Enter の受理を待つ上限と同じ accept_timeout・start_timeout のうち長いほう（既定 30 秒）にする（P-102 と同じ考え方。P-109）。
func (o *Operator) reactionWindow() time.Duration {
	return max(o.deps.Cfg.AcceptTimeout, o.deps.Cfg.StartTimeout)
}
