package opstest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// lateExitingAgent は /exit の Enter のあと、状態の確認（list-panes）が polls 回あってからシェルに戻る擬似エージェント。
// polls が負なら戻らない。戻る前に送られた文字列（/exit 以外）を typed に集める（エージェントに入力されたことになる）。
func (f *fixture) lateExitingAgent(polls int) (typed *[]string) {
	var (
		last     string
		exited   bool
		returned bool
		lists    int
		got      []string
	)
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
			if o.Text != "/exit" && !returned {
				got = append(got, o.Text)
			}
		case o.Kind == "keys" && o.Keys[0] == "Enter":
			switch last {
			case "/exit":
				exited = true
			case "cd ~":
				f.becomeShell("終了しました\n~ $ ")
			case "clear":
				f.becomeShell("~ $ ")
			}
		}
	})
	f.env.Tmux.OnList(func() {
		if !exited || returned || polls < 0 {
			return
		}
		if lists++; lists > polls {
			returned = true
			f.becomeShell("終了しました\n" + panetest.ScreenShell())
		}
	})
	return &got
}

// 終了が遅れる omp でも、shell に戻るまで cd ~ 以降を送らない（Issue #30）。
func TestCloseWaitsForLateExit(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	typed := f.lateExitingAgent(5)
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil {
		t.Fatal(err)
	}
	if len(*typed) != 0 {
		t.Fatalf("終了前のエージェントに入力された: %v", *typed)
	}
	if !res.OK || res.State != agent.StateShell || !res.NoteArchived {
		t.Fatalf("res = %+v", res)
	}
	wantOps := []string{
		`literal %0 "/exit"`, "keys %0 Enter",
		`literal %0 "cd ~"`, "keys %0 Enter",
		`literal %0 "clear"`, "keys %0 Enter",
		`title %0 ""`,
	}
	if got := opStrings(f); !equalStrings(got, wantOps) {
		t.Fatalf("ops = %v", got)
	}
}

// 上限までに shell に戻らないときは cd ~ 以降を送らず exit_timeout。メモも最終報告も移さない（Issue #30）。
func TestCloseExitTimeoutKeepsNote(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	f.env.FS.Put("/reports/T1.md", "最終報告")
	typed := f.lateExitingAgent(-1) // 終了しない
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if code(err) != apperr.ExitTimeout {
		t.Fatalf("err = %v", err)
	}
	if res.OK || res.NoteArchived {
		t.Fatalf("res = %+v", res)
	}
	if len(*typed) != 0 {
		t.Fatalf("終了前のエージェントに入力された: %v", *typed)
	}
	if want := []string{`literal %0 "/exit"`, "keys %0 Enter"}; !equalStrings(opStrings(f), want) {
		t.Fatalf("ops = %v", opStrings(f))
	}
	if len(f.env.Notes.Archives()) != 0 {
		t.Fatalf("確認できないのにメモを移した: %v", f.env.Notes.Archives())
	}
	if !f.env.FS.Has("/reports/T1.md") {
		t.Fatal("確認できないのに最終報告を移した")
	}
}
