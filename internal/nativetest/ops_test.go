package nativetest

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// run は fn を別 goroutine で走らせ、fn が時計（After）で待つたびに Fake の時計を進める。固定の sleep は使わない。
func run[T any](t *testing.T, clk *clock.Fake, fn func() (T, error)) (T, error) {
	t.Helper()
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case r := <-ch:
			return r.v, r.err
		default:
		}
		if clk.Waiters() > 0 {
			clk.Advance(100 * time.Millisecond)
		} else {
			runtime.Gosched()
		}
		if time.Now().After(deadline) {
			t.Fatal("操作が終わりません")
		}
	}
}

func (g *rig) nativeRef() pane.Ref { return pane.Ref{ID: nativeID, Pos: "T1", Local: "T1"} }

// onEnter は %0 に Enter が送られたときの擬似の omp の反応を差し込む（Enter ごとに 1 回呼ぶ）。
func (g *rig) onEnter(fn func(n int)) {
	count := 0
	g.env.Tmux.OnOp(func(o panetest.Op) {
		if o.ID == nativeID && o.Kind == "keys" && len(o.Keys) > 0 && o.Keys[len(o.Keys)-1] == "Enter" {
			count++
			fn(count)
		}
	})
}

// send_text の受理：tern の画面は capture-pane に何も出ないが、Enter のあとに指示が記録されて working と読めれば accepted。
func TestSendTextIsAcceptedOnTernScreen(t *testing.T) {
	g := newRig(t, 3, nil, nil, nil) // idle
	text := "調べて報告して\n2 行目"
	g.onEnter(func(int) { g.userSays(text) })
	res, err := run(t, g.env.Clock, func() (pane.SendResult, error) {
		return g.op.SendText(context.Background(), g.nativeRef(), text)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
	if _, marked := g.un.Since(nativeID); marked {
		t.Error("受理を確かめられたのに、受理の記録（Unaccepted）が残っている")
	}
}

// Enter を送っても記録に指示が現れない（受け付けていない）ペインは accepted:false。idle のままなら Enter を送り直す。
func TestSendTextIsNotAcceptedWhenNothingIsRecorded(t *testing.T) {
	g := newRig(t, 3, nil, nil, nil)
	res, err := run(t, g.env.Clock, func() (pane.SendResult, error) {
		return g.op.SendText(context.Background(), g.nativeRef(), "x")
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted || res.State != agent.StateIdle {
		t.Fatalf("res = %+v", res)
	}
	if n := g.env.Tmux.CountKeys("Enter"); n < 2 {
		t.Errorf("Enter = %d 回, want 送り直しを含めて 2 回以上", n)
	}
	if _, marked := g.un.Since(nativeID); !marked {
		t.Error("受理を確かめられなかったのに、記録が無い")
	}
}

// すでに作業中のペインへの send_text は、指示が記録されたこと（入力欄が見えないので）で受理を確かめる。記録されなければ accepted:false。
func TestSendTextWhileWorkingNeedsTheInstructionRecorded(t *testing.T) {
	text := "追加の指示です"
	for _, tc := range []struct {
		name     string
		recorded bool
	}{{"記録された", true}, {"記録されない", false}} {
		t.Run(tc.name, func(t *testing.T) {
			g := newRig(t, 4, nil, nil, nil) // 指示を受けて作業中
			if tc.recorded {
				g.onEnter(func(int) { g.userSays(text) })
			}
			res, err := run(t, g.env.Clock, func() (pane.SendResult, error) {
				return g.op.SendText(context.Background(), g.nativeRef(), text)
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Accepted != tc.recorded || res.State != agent.StateWorking {
				t.Fatalf("res = %+v, want accepted=%v state=working", res, tc.recorded)
			}
		})
	}
}

// ask_pick：tern の画面ではカーソルが見えないので、最初の位置からの差分のキーと Enter（と、Submit の Enter）を送り、回答が記録されて閉じる。
func TestAskPickOnTernScreen(t *testing.T) {
	g := newRig(t, 6, nil, nil, nil) // 質問が出ている（Red = 1 番にカーソル）
	g.onEnter(func(n int) {
		if n == 2 { // 2 回目の Enter（Submit）で回答が確定する
			g.appendFixture(7)
		}
	})
	ref := pane.Ref{ID: nativeID, Pos: "T1", Local: "T1"}
	res, err := run(t, g.env.Clock, func() (pane.PickResult, error) {
		return g.op.PickAsk(context.Background(), ref, 2, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.CardClosed || res.Picked == nil || res.Picked.Label != "Blue" || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
	got := strings.Join(g.env.Tmux.OpStrings(), " | ")
	if want := "keys %0 Down Enter | keys %0 Enter"; got != want {
		t.Errorf("送ったキー\n got=%s\nwant=%s", got, want)
	}
}

// 「その他」への自由入力は、tern の画面では入力欄が開いたことを確かめられないので、キーを 1 つも送らずに断る。
func TestAskPickCustomTextIsRefusedOnTernScreen(t *testing.T) {
	g := newRig(t, 6, nil, nil, nil)
	_, err := run(t, g.env.Clock, func() (pane.PickResult, error) {
		return g.op.PickAsk(context.Background(), g.nativeRef(), 0, "自由入力")
	})
	if err == nil || !strings.Contains(err.Error(), "custom_not_allowed") {
		t.Fatalf("err = %v, want custom_not_allowed", err)
	}
	if ops := g.env.Tmux.OpStrings(); len(ops) != 0 {
		t.Errorf("キーを送った: %v", ops)
	}
}
