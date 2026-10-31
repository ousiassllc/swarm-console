package opstest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// 最初の送信（貼り付け・文字列）の直後にクライアントが切断（ctx cancel）しても、
// そのキー列の Enter は送りきる（半端な入力をペインに残さない。NFR-AVL-7）。

func TestSendTextCancelAfterPasteStillSendsEnter(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	ctx, cancel := context.WithCancel(context.Background())
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "paste" {
			cancel()
		}
	})
	_, _ = run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(ctx, ref, "指示") })
	if got := opStrings(f); !equalStrings(got, []string{`paste %0 "指示"`, "keys %0 Enter"}) {
		t.Fatalf("ops = %v", got)
	}
}

func TestDispatchCancelAfterPasteStillSendsEnterAndReportsSent(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	ctx, cancel := context.WithCancel(context.Background())
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "paste" {
			cancel()
		}
	})
	res, _ := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(ctx, ref, pane.DispatchArgs{Agent: "omp", Text: instruction})
	})
	if f.env.Tmux.CountKeys("Enter") != 1 || !res.Sent {
		t.Fatalf("ops = %v res = %+v", opStrings(f), res)
	}
}

// shell と見えたペインではなく idle だったとき、Started=false でも Sent=true になる。
func TestDispatchSentFlag(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"}) // working にならない＝受理されない
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: instruction})
	})
	if err != nil || res.Accepted || res.Started || !res.Sent {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// 事前拒否では Sent=false。
	ref = f.single(panetest.ScreenWorking("omp", "1", "x"), []string{"omp"})
	res, err = f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: "x"})
	if err == nil || res.Sent {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// 各文字列の直後に cancel しても、その Enter は欠けない。
func TestCloseCancelNeverLeavesTextWithoutEnter(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	ctx, cancel := context.WithCancel(context.Background())
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "literal" {
			cancel()
		}
	})
	_, _ = run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(ctx, ref) })
	assertEveryLiteralHasEnter(t, f)
}

func TestStartCancelNeverLeavesTextWithoutEnter(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "literal" {
			cancel()
		}
	})
	_, _ = run(t, f.env.Clock, func() (pane.StartResult, error) { return f.op.Start(ctx, ref, "omp", "/work", "t") })
	assertEveryLiteralHasEnter(t, f)
	if n := f.env.Tmux.CountKeys("Enter"); n != 2 {
		t.Fatalf("cd と起動コマンドの Enter が欠けた: ops = %v", opStrings(f))
	}
}

func assertEveryLiteralHasEnter(t *testing.T, f *fixture) {
	t.Helper()
	ops := f.env.Tmux.Ops()
	lits := 0
	for i, o := range ops {
		if o.Kind != "literal" {
			continue
		}
		lits++
		if i+1 >= len(ops) || ops[i+1].Kind != "keys" || ops[i+1].Keys[0] != "Enter" {
			t.Fatalf("%q の直後に Enter が無い: %v", o.Text, opStrings(f))
		}
	}
	if lits == 0 {
		t.Fatal("文字列が 1 つも送られていない")
	}
}
