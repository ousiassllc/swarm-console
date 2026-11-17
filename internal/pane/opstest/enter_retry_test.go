package opstest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// resendStep は ops.enterRetryStep（Enter を送り直す間隔）。
const resendStep = 2 * time.Second

// shortRequest は貼り付けの確認メニューが開かない長さ（largePasteLines 未満）の依頼文。
func shortRequest() string {
	return strings.TrimSuffix(strings.Repeat("短めの依頼文の 1 行です。\n", 30), "\n")
}

// 入力欄に本文が残ったまま Enter が取りこぼされても（Issue #101）、受理されるまで Enter を送り直して受理される。
func TestSendTextResendsEnterWhenFirstEnterIsDropped(t *testing.T) {
	f := newFixture(t)
	ref, p := f.ompLargePaste(t, false)
	p.dropEnters = 1
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, shortRequest()) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
	if n := f.env.Tmux.CountKeys("Enter"); n != 2 {
		t.Fatalf("Enter を %d 回送った（取りこぼされた 1 回 + 送り直し 1 回で 2 回）", n)
	}
	if got := f.env.Clock.Now().Sub(panetest.T0); got > time.Second+f.env.Cfg.AcceptTimeout+time.Second {
		t.Fatalf("決着が 1 秒 + accept_timeout を超えた: %v（NFR-PERF-4）", got)
	}
}

// dispatch も同じ。報告された症状（ok:false / accepted:false / state:idle で入力欄に本文が残る）にならない。
func TestDispatchResendsEnterWhenFirstEnterIsDropped(t *testing.T) {
	f := newFixture(t)
	ref, p := f.ompLargePaste(t, false)
	p.dropEnters = 2
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: shortRequest()})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || !res.Sent || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
	if n := f.env.Tmux.CountKeys("Enter"); n != 3 {
		t.Fatalf("Enter を %d 回送った（取りこぼされた 2 回 + 送り直し 1 回で 3 回）", n)
	}
}

// 初回の Enter で受理されたときは Enter を送り直さない。
func TestSendTextAcceptedFirstEnterIsNotResent(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, false)
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, shortRequest()) })
	if err != nil || !res.OK || !res.Accepted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := f.env.Tmux.CountKeys("Enter"); n != 1 {
		t.Fatalf("Enter を %d 回送った", n)
	}
}

// Enter を送り直しても受理されないときは、これまでどおり ok:false を返す。送り直しは間隔を空け、期限（1 秒 + accept_timeout）を延ばさない。
func TestDispatchNeverAcceptedStaysNotOKWithBoundedResends(t *testing.T) {
	f := newFixture(t)
	ref, p := f.ompLargePaste(t, false)
	p.dropEnters = 1 << 30
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: shortRequest()})
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted || !res.Sent {
		t.Fatalf("受理されないのに成功にした、または sent を落とした: %+v", res)
	}
	if res.State != agent.StateIdle {
		t.Fatalf("state = %q", res.State)
	}
	if n, limit := f.env.Tmux.CountKeys("Enter"), 1+int(f.env.Cfg.AcceptTimeout/resendStep); n < 2 || n > limit {
		t.Fatalf("Enter を %d 回送った（2〜%d 回のはず）", n, limit)
	}
	if got := f.env.Clock.Now().Sub(panetest.T0); got > time.Second+f.env.Cfg.AcceptTimeout+time.Second {
		t.Fatalf("決着が 1 秒 + accept_timeout を超えた: %v（NFR-PERF-4）", got)
	}
}
