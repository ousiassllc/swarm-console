package opstest

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// Issue #120: 作業中のペインに送った指示への反応（実行した・無視した・確かめられなかった）を、会話の記録から返す。

const agentPID = 101 // panetest の Pane(%0, 100, ...) のプロセス木で、エージェント（argv の 2 つ目）の pid

// workingRig は、作業中（working）の omp が 1 つ（%0）あり、会話の記録の読み取りを差し替えられる fixture。
func workingRig(t *testing.T) (*fixture, *panetest.ProbeAdapter, pane.Ref) {
	t.Helper()
	probe := panetest.NewProbeAdapter("omp")
	f := newFixtureWith(t, func(env *panetest.Env) { env.Adapters["omp"] = probe })
	ref := f.single(panetest.ScreenWorking("omp", "1", "処理"), []string{"omp"})
	// 貼った本文が入力欄に載り、Enter で入力欄が空になる（working のまま）。
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch o.Kind {
		case "paste":
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "処理")+o.Text)
		case "keys":
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "2", "処理"))
		}
	})
	return f, probe, ref
}

func sendWhileWorking(t *testing.T, f *fixture, ref pane.Ref, text string) pane.SendResult {
	t.Helper()
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// 画面が受理を映さなくても（描き直しが止まった omp）、記録で実行と分かれば ok・accepted は true。無視・確かめられないときは受理のとおり。
func TestSendTextActedOverridesUnconfirmedScreen(t *testing.T) {
	for _, acted := range []bool{true, false} {
		f, probe, ref := workingRig(t)
		f.env.Tmux.OnOp(func(o panetest.Op) { // 入力欄が空にならない（受理を画面で確かめられない）
			if o.Kind == "paste" {
				f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "処理")+o.Text)
			}
		})
		if acted {
			probe.SetReaction(agentPID, agent.ReactionReading{Reaction: agent.ReactionActed, Basis: agent.BasisUserMessage, Final: true})
		}
		res := sendWhileWorking(t, f, ref, "追加の指示です")
		if res.OK != acted || res.Accepted != acted {
			t.Errorf("acted=%v: res = %+v", acted, res)
		}
	}
}

func TestSendTextWhileWorkingReportsReaction(t *testing.T) {
	cases := []struct {
		name     string
		reading  agent.ReactionReading
		want     agent.Reaction
		basis    string
		maxAfter time.Duration // 貼り付けから返るまでの上限（Final なら待たずに返る）
	}{
		{"実行した", agent.ReactionReading{Reaction: agent.ReactionActed, Basis: agent.BasisUserMessage, Final: true},
			agent.ReactionActed, agent.BasisUserMessage, 3 * time.Second},
		{"無視した", agent.ReactionReading{Reaction: agent.ReactionIgnored, Basis: agent.BasisToolOutput, Final: true},
			agent.ReactionIgnored, agent.BasisToolOutput, 3 * time.Second},
		{"確かめられなかった（記録に現れない）", agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisNotFound},
			agent.ReactionUnverified, agent.BasisNotFound, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, probe, ref := workingRig(t)
			probe.SetReaction(agentPID, tc.reading)
			res := sendWhileWorking(t, f, ref, "追加の指示です")
			if !res.OK || !res.Accepted || res.State != agent.StateWorking || res.Reaction != tc.want || res.ReactionBasis != tc.basis {
				t.Fatalf("res = %+v", res)
			}
			elapsed := f.env.Clock.Now().Sub(panetest.T0)
			if tc.maxAfter > 0 && elapsed > tc.maxAfter {
				t.Errorf("反応が決まっているのに %v かかった", elapsed)
			}
			if tc.maxAfter == 0 && elapsed < f.env.Cfg.StartTimeout {
				t.Errorf("確かめられないときは上限（%v）まで待つ。%v で返った", f.env.Cfg.StartTimeout, elapsed)
			}
			calls := probe.ReactionCalls()
			if len(calls) == 0 || calls[0].PID != agentPID || calls[0].Text != "追加の指示です" || calls[0].Since.Before(panetest.T0) {
				t.Errorf("記録の読み取り = %+v", calls)
			}
		})
	}
}

// 受け取ったが応答がまだ、のあとで実行された、と記録が進めば、待つ間に実行したと返す。
func TestSendTextWhileWorkingWaitsForReactionToSettle(t *testing.T) {
	f, probe, ref := workingRig(t)
	probe.SetReaction(agentPID, agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisUserMessageNoAns})
	probe.SetReactionAfter(agentPID, 5, agent.ReactionReading{Reaction: agent.ReactionActed, Basis: agent.BasisUserMessage, Final: true}) // 5 回目の読み取りで応答が記録される
	enters := 0
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "keys" {
			enters++
		}
	})
	res := sendWhileWorking(t, f, ref, "追加の指示です")
	if res.Reaction != agent.ReactionActed {
		t.Fatalf("res = %+v", res)
	}
	if enters != 1 {
		t.Errorf("Enter %d 回（作業中のペインに送り直さない）", enters)
	}
	if n := len(probe.ReactionCalls()); n != 5 {
		t.Errorf("記録を %d 回読んだ（実行と分かった 5 回目で止める）", n)
	}
}

// idle のペインへの送信は今までどおり。反応は読まず、結果にも出さない。
func TestSendTextIdleDoesNotReadReaction(t *testing.T) {
	probe := panetest.NewProbeAdapter("omp")
	f := newFixtureWith(t, func(env *panetest.Env) { env.Adapters["omp"] = probe })
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "keys" {
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "処理"))
		}
	})
	res := sendWhileWorking(t, f, ref, "指示")
	if !res.OK || !res.Accepted || res.Reaction != "" || res.ReactionBasis != "" {
		t.Fatalf("res = %+v", res)
	}
	if n := len(probe.ReactionCalls()); n != 0 {
		t.Errorf("idle への送信で記録を %d 回読んだ", n)
	}
}

// 会話の記録を読めないエージェント（アダプタが ReactionProbe を持たない）は、待たずに「確かめられなかった」。
func TestSendTextWhileWorkingUnsupportedAgent(t *testing.T) {
	f := newFixture(t) // 既定は FakeAdapter（ReactionProbe なし）
	ref := f.single(panetest.ScreenWorking("omp", "1", "処理"), []string{"omp"})
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "keys" {
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "2", "処理"))
		}
	})
	res := sendWhileWorking(t, f, ref, "追加の指示です")
	if !res.OK || res.Reaction != agent.ReactionUnverified || res.ReactionBasis != agent.BasisUnsupported {
		t.Fatalf("res = %+v", res)
	}
}
