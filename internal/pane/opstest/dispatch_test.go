package opstest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

const instruction = "/sweep\n\n## やること\nやる\n\n## 条件\n- 条件"

func dispatchArgs() pane.DispatchArgs {
	return pane.DispatchArgs{Agent: "omp", Cwd: "/mnt/nvme/w", Title: "funid #1", Text: instruction}
}

// acceptOnEnter は Enter で working になる擬似エージェントを仕込む（起動コマンドの Enter では何もしない）。
func (f *fixture) acceptOnPaste() {
	pasted := false
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "paste":
			pasted = true
		case o.Kind == "keys" && o.Keys[0] == "Enter" && pasted:
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "指示を実行"))
		}
	})
}

func TestDispatchShellStartsThenSends(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	f.env.FS.Put("/reports/T1.md", "古い報告")
	var last string
	pasted := false
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "paste":
			pasted = true
		case o.Kind == "keys" && o.Keys[0] == "Enter" && pasted:
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "指示を実行"))
		case o.Kind == "keys" && o.Keys[0] == "Enter" && last == "omp":
			f.becomeAgent([]string{"omp"}, panetest.ScreenIdle("omp"))
		}
	})
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, dispatchArgs())
	})
	if err != nil {
		t.Fatal(err)
	}
	want := pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Started: true}
	if res != want {
		t.Fatalf("res = %+v", res)
	}
	wantOps := []string{
		`title %0 "kana: funid #1"`,
		`literal %0 "cd '/mnt/nvme/w'"`, "keys %0 Enter",
		`literal %0 "omp"`, "keys %0 Enter",
		`paste %0 "` + "/sweep\\n\\n## やること\\nやる\\n\\n## 条件\\n- 条件" + `"`, "keys %0 Enter",
	}
	if got := opStrings(f); !equalStrings(got, wantOps) {
		t.Fatalf("ops = %v\nwant %v", got, wantOps)
	}
	if f.env.FS.Has("/reports/T1.md") || len(f.env.FS.Files()) != 1 {
		t.Fatalf("古い最終報告を archive へ移していない: %v", f.env.FS.Files())
	}
}

func TestDispatchIdleSendsDirectly(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.FS.Put("/reports/T1.md", "古い報告")
	f.acceptOnPaste()
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: instruction})
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := (pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true}); res != want {
		t.Fatalf("res = %+v", res)
	}
	if got := opStrings(f); len(got) != 2 || got[1] != "keys %0 Enter" {
		t.Fatalf("ops = %v", got)
	}
	if f.env.FS.Has("/reports/T1.md") {
		t.Fatal("古い最終報告が残っている")
	}
}

func TestDispatchNotAccepted(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"}) // working にならない
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: instruction})
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted || res.Started || res.State != agent.StateIdle {
		t.Fatalf("res = %+v", res)
	}
}

func TestDispatchRejectsWithoutSending(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cases := []struct {
		name   string
		screen string
		argv   []string
		args   pane.DispatchArgs
		want   apperr.Code
	}{
		{"working", panetest.ScreenWorking("omp", "1", "x"), []string{"omp"}, pane.DispatchArgs{Agent: "omp", Text: "x"}, apperr.InvalidState},
		{"asking", panetest.ScreenAsk("omp", "q", 1, false, "A"), []string{"omp"}, pane.DispatchArgs{Agent: "omp", Text: "x"}, apperr.InvalidState},
		{"idle の実エージェントと違う", panetest.ScreenIdle("omp"), []string{"omp"}, pane.DispatchArgs{Agent: "claude", Text: "x"}, apperr.InvalidArgs},
		{"未登録のエージェント", panetest.ScreenShell(), nil, pane.DispatchArgs{Agent: "gpt", Cwd: "/w", Title: "t", Text: "x"}, apperr.InvalidArgs},
		{"shell で cwd 無し", panetest.ScreenShell(), nil, pane.DispatchArgs{Agent: "omp", Title: "t", Text: "x"}, apperr.InvalidArgs},
		{"shell で cwd に '", panetest.ScreenShell(), nil, pane.DispatchArgs{Agent: "omp", Cwd: "/a'b", Title: "t", Text: "x"}, apperr.InvalidArgs},
		{"shell で title 無し", panetest.ScreenShell(), nil, pane.DispatchArgs{Agent: "omp", Cwd: "/w", Text: "x"}, apperr.InvalidArgs},
		{"制御文字", panetest.ScreenIdle("omp"), []string{"omp"}, pane.DispatchArgs{Agent: "omp", Text: "a\x1b"}, apperr.ForbiddenControlChar},
	}
	for _, tc := range cases {
		ref := f.single(tc.screen, tc.argv)
		_, err := f.op.Dispatch(ctx, ref, tc.args)
		if code(err) != tc.want {
			t.Errorf("%s: err = %v, want %s", tc.name, err, tc.want)
		}
		if tc.want == apperr.InvalidArgs && tc.name == "idle の実エージェントと違う" {
			if ae := apperr.As(err); ae.Details["field"] != "agent" {
				t.Errorf("details = %v", ae.Details)
			}
		}
	}
	if ops := f.env.Tmux.Ops(); len(ops) != 0 {
		t.Fatalf("拒否のときは何も送らない: %v", ops)
	}
}

func TestDispatchStartBlockedByAsking(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	var last string
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "literal" {
			last = o.Text
		}
		if o.Kind == "keys" && o.Keys[0] == "Enter" && last == "omp" {
			f.becomeAgent([]string{"omp"}, panetest.ScreenAsk("omp", "信頼しますか？", 1, false, "はい"))
		}
	})
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, dispatchArgs())
	})
	if err != nil || res.OK || res.Accepted || res.State != agent.StateAsking {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	for _, o := range f.env.Tmux.Ops() {
		if o.Kind == "paste" {
			t.Fatal("起動が終わらないのに指示を貼った")
		}
	}
}
