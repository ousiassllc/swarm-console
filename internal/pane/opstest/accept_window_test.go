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

// startingOmp は shell のペインで omp の起動コマンドを受けると idle の画面を出すが、
// 起動から initDelay（偽の時計）がたつまでは Enter を送信として受け付けない擬似 omp（Issue #103）。
// 実機では、高負荷のとき画面に入力待ちが出てからも初期化が続き、その間の Enter が取りこぼされる。
func (f *fixture) startingOmp(initDelay time.Duration) pane.Ref {
	ref := f.single(panetest.ScreenShell(), nil)
	var last string
	var readyAt time.Time
	pasted := false
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "paste":
			pasted = true
		case o.Kind == "keys" && o.Keys[0] == "Enter" && last == "omp" && !pasted:
			readyAt = f.env.Clock.Now().Add(initDelay)
			f.becomeAgent([]string{"omp"}, panetest.ScreenIdle("omp"))
		case o.Kind == "keys" && o.Keys[0] == "Enter" && pasted && !f.env.Clock.Now().Before(readyAt):
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "指示を実行"))
		}
	})
	return ref
}

// omp を起動した直後の dispatch は、初期化が accept_timeout（10 秒）より長引いても、受理されるまで待つ。
// 直す前は accept_timeout で打ち切られ、ok:false / accepted:false / state:idle になっていた。
func TestDispatchJustStartedWaitsPastAcceptTimeoutForSlowInit(t *testing.T) {
	f := newFixture(t)
	init := f.env.Cfg.AcceptTimeout + 5*time.Second
	ref := f.startingOmp(init)
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, dispatchArgs())
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || !res.Started || !res.Sent || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
}

// 起動直後でも受理を確かめられなければ、待ちの上限（1 秒 + start_timeout）で ok:false を返す。送ったふりはしない。
func TestDispatchJustStartedNeverAcceptedStaysNotOKWithinStartTimeout(t *testing.T) {
	f := newFixture(t)
	ref := f.startingOmp(time.Hour)
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, dispatchArgs())
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted || !res.Started || !res.Sent || res.State != agent.StateIdle {
		t.Fatalf("受理されないのに成功にした、または sent・started を落とした: %+v", res)
	}
	// 起動の待ち（スナップショットの取り直し 500 ms 刻み）は短く、そのあと 1 秒 + start_timeout。
	if got, limit := f.env.Clock.Now().Sub(panetest.T0), time.Second+f.env.Cfg.StartTimeout+2*time.Second; got > limit {
		t.Fatalf("決着が 1 秒 + start_timeout を超えた: %v", got)
	}
}

// 起動済みの idle のペインは延ばさない: 同じ遅れでも accept_timeout で ok:false（待ちの延長は起動した直後だけ）。
func TestDispatchAlreadyIdleKeepsAcceptTimeout(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	readyAt := panetest.T0.Add(f.env.Cfg.AcceptTimeout + 5*time.Second)
	pasted := false
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "paste":
			pasted = true
		case o.Kind == "keys" && o.Keys[0] == "Enter" && pasted && !f.env.Clock.Now().Before(readyAt):
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "指示を実行"))
		}
	})
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: instruction})
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted || res.Started || !res.Sent || res.State != agent.StateIdle {
		t.Fatalf("res = %+v", res)
	}
	if got, limit := f.env.Clock.Now().Sub(panetest.T0), time.Second+f.env.Cfg.AcceptTimeout+time.Second; got > limit {
		t.Fatalf("決着が 1 秒 + accept_timeout を超えた: %v", got)
	}
}

// Enter を送り直したら、回数と経過時間をログに残す。依頼文の中身は出さない。
func TestEnterResendIsLoggedWithCountAndElapsed(t *testing.T) {
	f := newFixture(t)
	ref, p := f.ompLargePaste(t, false)
	p.dropEnters = 2
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, shortRequest()) })
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var resends []string
	for _, l := range strings.Split(f.logs.String(), "\n") {
		if strings.Contains(l, "Enter を送り直しました") {
			resends = append(resends, l)
		}
	}
	if len(resends) != 2 {
		t.Fatalf("送り直しのログが %d 行: %q", len(resends), f.logs.String())
	}
	for i, want := range []string{"attempt=1", "attempt=2"} {
		if !strings.Contains(resends[i], want) || !strings.Contains(resends[i], "elapsed=") || !strings.Contains(resends[i], "pane=%0") {
			t.Fatalf("ログ %d 行目に %s・elapsed・pane が無い: %s", i+1, want, resends[i])
		}
	}
	if !strings.Contains(f.logs.String(), "elapsed=2s") || !strings.Contains(f.logs.String(), "elapsed=4s") {
		t.Fatalf("経過時間（最初の Enter から 2 秒・4 秒）が出ていない: %s", f.logs.String())
	}
	if strings.Contains(f.logs.String(), "短めの依頼文") {
		t.Fatalf("依頼文の中身がログに出た: %s", f.logs.String())
	}
}

// 1 回で受理されたときは、送り直しのログを出さない。
func TestEnterNotResentWritesNoResendLog(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, false)
	if res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, shortRequest()) }); err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if strings.Contains(f.logs.String(), "送り直") {
		t.Fatalf("送り直していないのにログが出た: %s", f.logs.String())
	}
}
