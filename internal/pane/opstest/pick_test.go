package opstest

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

func code(err error) apperr.Code {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// askFixture は A・B・C の 3 択（自由入力あり）の質問カードのペインを作る。キーに反応する擬似エージェントを仕込む。
// cursor はカーソル位置（1 始まり）、onEnter は Enter が押されたときの画面を返す。
type askAgent struct {
	cursor  int
	custom  bool
	onEnter func(cursor int) string
}

func (f *fixture) ask(custom bool, onEnter func(cursor int) string) (pane.Ref, *askAgent) {
	a := &askAgent{cursor: 1, custom: custom, onEnter: onEnter}
	ref := f.single(a.screen(), []string{"omp"})
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind != "keys" {
			return
		}
		for _, k := range o.Keys {
			switch k {
			case "Down":
				a.cursor++
			case "Up":
				a.cursor--
			case "Enter":
				f.env.Tmux.SetScreen("%0", a.onEnter(a.cursor))
				return
			}
		}
		f.env.Tmux.SetScreen("%0", a.screen())
	})
	return ref, a
}

func (a *askAgent) screen() string {
	return panetest.ScreenAsk("omp", "どれ？", a.cursor, a.custom, "A", "B", "C")
}

func working() string { return panetest.ScreenWorking("omp", "1", "続き") }

func TestPickAskByNumber(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ask(true, func(int) string { return working() })
	res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 3, "") })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.State != agent.StateWorking || !res.CardClosed || res.Picked == nil || res.Picked.N != 3 || res.Picked.Label != "C" {
		t.Fatalf("res = %+v", res)
	}
	want := []string{"keys %0 Down Down", "keys %0 Enter"}
	if got := opStrings(f); !equalStrings(got, want) {
		t.Fatalf("ops = %v", got)
	}
}

func TestPickAskRejectsWithoutSending(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ask(false, func(int) string { return working() })
	ctx := context.Background()
	cases := []struct {
		name string
		n    int
		text string
		want apperr.Code
	}{
		{"n も text も無い", 0, "", apperr.InvalidArgs},
		{"n と text の同時指定", 1, "x", apperr.InvalidArgs},
		{"範囲外（大きい）", 4, "", apperr.OptionOutOfRange},
		{"範囲外（負）", -1, "", apperr.OptionOutOfRange},
		{"自由入力できないカード", 0, "自由", apperr.CustomNotAllowed},
		{"制御文字", 0, "a\x1bb", apperr.ForbiddenControlChar},
	}
	for _, tc := range cases {
		_, err := f.op.PickAsk(ctx, ref, tc.n, tc.text)
		if code(err) != tc.want {
			t.Errorf("%s: err = %v, want %s", tc.name, err, tc.want)
		}
	}
	if ops := f.env.Tmux.Ops(); len(ops) != 0 {
		t.Fatalf("拒否のときは何も送らない: %v", ops)
	}
}

func TestPickAskNotAskingAndUnsupported(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	if _, err := f.op.PickAsk(context.Background(), ref, 1, ""); code(err) != apperr.NotAsking {
		t.Fatalf("idle: %v", err)
	}
	f.becomeShell(panetest.ScreenShell())
	if _, err := f.op.PickAsk(context.Background(), ref, 1, ""); code(err) != apperr.NotAsking {
		t.Fatalf("shell: %v", err)
	}
	f.env.Tmux.SetScreen("%0", "何か")
	f.env.Host.SetTree(100, []string{"bash"}, []string{"vim"})
	p := f.env.Pane("%0", 100, 0, 0, "何か", nil)
	p.CurrentCmd = "vim"
	f.env.Tmux.SetPanes(p)
	f.env.Host.SetTree(100, []string{"bash"}, []string{"vim"})
	if _, err := f.op.PickAsk(context.Background(), ref, 1, ""); code(err) != apperr.UnsupportedAgent {
		t.Fatalf("unknown: %v", err)
	}
	if len(f.env.Tmux.Ops()) != 0 {
		t.Fatal("何も送らない")
	}
}

func TestPickAskCursorMismatchNeverSendsEnter(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenAsk("omp", "どれ？", 1, true, "A", "B", "C"), []string{"omp"})
	// キーを送ってもカーソルが動かない画面。
	_, err := f.op.PickAsk(context.Background(), ref, 3, "")
	if code(err) != apperr.CursorMismatch {
		t.Fatalf("err = %v", err)
	}
	if n := f.env.Tmux.CountKeys("Enter"); n != 0 {
		t.Fatalf("Enter を %d 回送った", n)
	}
	downs := 0
	for _, o := range f.env.Tmux.Ops() {
		if o.Kind == "keys" {
			downs++
		}
	}
	if downs != 3 {
		t.Fatalf("キー列の送信は最大 3 回: %d", downs)
	}
}

func TestPickAskRetriesUntilCursorMatches(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenAsk("omp", "どれ？", 1, true, "A", "B", "C"), []string{"omp"})
	// 1 回目のキーは取りこぼされ、2 回目で反映される。
	presses := 0
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "keys" && o.Keys[0] == "Down":
			presses++
			if presses == 2 {
				f.env.Tmux.SetScreen("%0", panetest.ScreenAsk("omp", "どれ？", 2, true, "A", "B", "C"))
			}
		case o.Kind == "keys" && o.Keys[0] == "Enter":
			f.env.Tmux.SetScreen("%0", working())
		}
	})
	res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 2, "") })
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := opStrings(f); !equalStrings(got, []string{"keys %0 Down", "keys %0 Down", "keys %0 Enter"}) {
		t.Fatalf("ops = %v", got)
	}
}

func TestPickAskCardDoesNotClose(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ask(true, func(cursor int) string { return panetest.ScreenAsk("omp", "どれ？", cursor, true, "A", "B", "C") })
	res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 2, "") })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.CardClosed || res.State != agent.StateAsking || res.Picked == nil || res.Picked.N != 2 {
		t.Fatalf("res = %+v", res)
	}
}

func TestPickAskCustomText(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ask(true, func(int) string { return working() })
	res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 0, "自由な答え") })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Picked != nil || res.State != agent.StateWorking || !res.CardClosed {
		t.Fatalf("res = %+v", res)
	}
	want := []string{"keys %0 Down Down Down", `paste %0 "自由な答え"`, "keys %0 Enter"}
	if got := opStrings(f); !equalStrings(got, want) {
		t.Fatalf("ops = %v", got)
	}
}

func TestPickAskCustomInputBoxOpensAfterEnter(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenAsk("omp", "どれ？", 3, true, "A", "B"), []string{"omp"})
	enters := 0
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "keys" && o.Keys[0] == "Down" {
			f.env.Tmux.SetScreen("%0", panetest.ScreenAsk("omp", "どれ？", 3, true, "A", "B"))
		}
		if o.Kind == "keys" && o.Keys[0] == "Enter" {
			enters++
			if enters == 1 { // 最初の Enter では自由入力欄が別に開く（選択肢は読めない）
				f.env.Tmux.SetScreen("%0", "[omp] ASK\nCustom answer:\n> ")
			} else {
				f.env.Tmux.SetScreen("%0", working())
			}
		}
	})
	f.env.Tmux.SetScreen("%0", panetest.ScreenAsk("omp", "どれ？", 2, true, "A", "B"))
	res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 0, "答え") })
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	want := []string{"keys %0 Down", `paste %0 "答え"`, "keys %0 Enter", `paste %0 "答え"`, "keys %0 Enter"}
	if got := opStrings(f); !equalStrings(got, want) {
		t.Fatalf("ops = %v", got)
	}
}

func TestPickAskBusyPane(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ask(true, func(int) string { return working() })
	unlock, _ := f.op.Lock(context.Background(), ref.ID)
	defer unlock()
	_, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 1, "") })
	if code(err) != apperr.BusyTimeout {
		t.Fatalf("err = %v", err)
	}
}
