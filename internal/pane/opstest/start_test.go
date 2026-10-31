package opstest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// shellThatStarts は shell のペインを作り、起動コマンドを打つと擬似エージェントが起動するようにする。
// start の中身は起動後の画面。"" ならエージェントは起動しない。
func (f *fixture) shellThatStarts(startScreen string) pane.Ref {
	ref := f.single(panetest.ScreenShell(), nil)
	var last string
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "keys" && o.Keys[0] == "Enter" && last == "omp" && startScreen != "":
			f.becomeAgent([]string{"omp"}, startScreen)
		}
	})
	return ref
}

func TestStart(t *testing.T) {
	f := newFixture(t)
	ref := f.shellThatStarts(panetest.ScreenIdle("omp"))
	res, err := run(t, f.env.Clock, func() (pane.StartResult, error) {
		return f.op.Start(context.Background(), ref, "omp", "/mnt/nvme/work dir", "funid #982")
	})
	if err != nil {
		t.Fatal(err)
	}
	want := pane.StartResult{OK: true, State: agent.StateIdle, Agent: "omp", Cwd: "/mnt/nvme/work dir", Title: "kana: funid #982"}
	if res != want {
		t.Fatalf("res = %+v, want %+v", res, want)
	}
	wantOps := []string{
		`title %0 "kana: funid #982"`,
		`literal %0 "cd '/mnt/nvme/work dir'"`, "keys %0 Enter",
		`literal %0 "omp"`, "keys %0 Enter",
	}
	if got := opStrings(f); !equalStrings(got, wantOps) {
		t.Fatalf("ops = %v", got)
	}
}

func TestStartTimesOutWaitingForIdle(t *testing.T) {
	f := newFixture(t)
	ref := f.shellThatStarts("") // 起動しない
	res, err := run(t, f.env.Clock, func() (pane.StartResult, error) {
		return f.op.Start(context.Background(), ref, "omp", "/work", "t")
	})
	if err != nil || res.OK || res.State != agent.StateShell {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if f.env.Clock.Now().Sub(panetest.T0) < f.env.Cfg.StartTimeout {
		t.Fatal("start_timeout より前に諦めた")
	}
}

func TestStartStopsWaitingWhenAsking(t *testing.T) {
	f := newFixture(t)
	ref := f.shellThatStarts(panetest.ScreenAsk("omp", "このフォルダを信頼しますか？", 1, false, "はい", "いいえ"))
	res, err := run(t, f.env.Clock, func() (pane.StartResult, error) {
		return f.op.Start(context.Background(), ref, "omp", "/work", "t")
	})
	if err != nil || res.OK || res.State != agent.StateAsking {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if elapsed := f.env.Clock.Now().Sub(panetest.T0); elapsed >= f.env.Cfg.StartTimeout {
		t.Fatalf("asking でも start_timeout まで待った: %v", elapsed)
	}
}

func TestStartRejections(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	cases := []struct {
		name              string
		agent, cwd, title string
		want              apperr.Code
	}{
		{"未登録のエージェント", "gpt", "/work", "t", apperr.InvalidArgs},
		{"cwd に '", "omp", "/work/it's", "t", apperr.InvalidArgs},
		{"cwd に制御文字", "omp", "/work/\n", "t", apperr.InvalidArgs},
		{"cwd が空", "omp", "", "t", apperr.InvalidArgs},
		{"title が空", "omp", "/work", "", apperr.InvalidArgs},
		{"title が長い", "omp", "/work", string(make([]rune, 81)) + "x", apperr.InvalidArgs},
		{"title に制御文字", "omp", "/work", "a\x07b", apperr.InvalidArgs},
	}
	for _, tc := range cases {
		if _, err := f.op.Start(ctx, ref, tc.agent, tc.cwd, tc.title); code(err) != tc.want {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	// 80 字ちょうどは通る。
	long := ""
	for range 80 {
		long += "あ"
	}
	_, err := run(t, f.env.Clock, func() (pane.StartResult, error) { return f.op.Start(ctx, ref, "omp", "/work", long) })
	if code(err) == apperr.InvalidArgs {
		t.Errorf("80 字は通るはず: %v", err)
	}
}

func TestStartRequiresShell(t *testing.T) {
	f := newFixture(t)
	for name, screen := range map[string]string{
		"idle":    panetest.ScreenIdle("omp"),
		"working": panetest.ScreenWorking("omp", "1", "x"),
		"asking":  panetest.ScreenAsk("omp", "q", 1, false, "A"),
	} {
		ref := f.single(screen, []string{"omp"})
		_, err := f.op.Start(context.Background(), ref, "omp", "/work", "t")
		if code(err) != apperr.InvalidState {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(f.env.Tmux.Ops()) != 0 {
		t.Fatalf("何も送らない: %v", f.env.Tmux.Ops())
	}
}
