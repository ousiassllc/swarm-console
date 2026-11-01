package opstest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/claude"
	"github.com/ousiassllc/swarm-console/internal/agent/codex"
	"github.com/ousiassllc/swarm-console/internal/agent/omp"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// closingAgent は終了コマンドでシェルに戻り、clear で画面が空になる擬似エージェント。
// stuck なら /exit しても終了しない（画面が片付かない）。
func (f *fixture) closingAgent(stuck bool) (times *[]time.Time) {
	var last string
	var stamps []time.Time
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
			stamps = append(stamps, f.env.Clock.Now())
		case o.Kind == "keys" && o.Keys[0] == "Enter" && !stuck:
			switch last {
			case "/exit":
				f.becomeShell("終了しました\n" + panetest.ScreenShell())
			case "cd ~":
				f.becomeShell("終了しました\n~ $ ")
			case "clear":
				f.becomeShell("~ $ ")
			}
		}
	})
	return &stamps
}

func TestCloseIdleOK(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	f.env.FS.Put("/reports/T1.md", "最終報告")
	times := f.closingAgent(false)
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.State != agent.StateShell || !res.NoteArchived {
		t.Fatalf("res = %+v", res)
	}
	if want := []string{"/exit", "cd ~", "clear", "title"}; !equalStrings(res.Steps, want) {
		t.Fatalf("steps = %v", res.Steps)
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
	// 「文字列 → 1 秒 → Enter → 1 秒」: 各文字列は 2 秒おき。
	if ts := *times; len(ts) != 3 || ts[1].Sub(ts[0]) < 2*time.Second || ts[2].Sub(ts[1]) < 2*time.Second {
		t.Fatalf("文字列の間隔: %v", ts)
	}
	a := f.env.Notes.Archives()
	if len(a) != 1 || a[0].ClosedBy != notes.ClosedByPaneClose || a[0].Meta.PaneID != "%0" || a[0].Meta.Pos != "T1" || a[0].Meta.Agent != "omp" {
		t.Fatalf("archives = %+v", a)
	}
	if f.env.FS.Has("/reports/T1.md") || len(f.env.FS.Files()) != 1 {
		t.Fatalf("最終報告が archive へ移っていない: %v", f.env.FS.Files())
	}
	if mode, ok := f.env.FS.DirMode("/reports/archive"); !ok || mode != 0o700 {
		t.Fatalf("archive の権限: %v %v", mode, ok)
	}
}

func TestCloseShellSkipsExit(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	f.closingAgent(false)
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if want := []string{"cd ~", "clear", "title"}; !equalStrings(res.Steps, want) {
		t.Fatalf("steps = %v", res.Steps)
	}
	if res.NoteArchived {
		t.Fatal("メモが無ければ note_archived=false")
	}
}

func TestCloseNotConfirmedKeepsNote(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	f.env.FS.Put("/reports/T1.md", "最終報告")
	f.closingAgent(true) // 終了しない
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.NoteArchived || res.State != agent.StateIdle {
		t.Fatalf("res = %+v", res)
	}
	if len(f.env.Notes.Archives()) != 0 {
		t.Fatalf("確認できないのにメモを移した: %v", f.env.Notes.Archives())
	}
	if !f.env.FS.Has("/reports/T1.md") {
		t.Fatal("確認できないのに最終報告を移した")
	}
}

func TestCloseRejectsOtherStates(t *testing.T) {
	f := newFixture(t)
	for name, screen := range map[string]string{
		"working": panetest.ScreenWorking("omp", "1", "x"),
		"asking":  panetest.ScreenAsk("omp", "q", 1, false, "A"),
	} {
		ref := f.single(screen, []string{"omp"})
		if _, err := f.op.Close(context.Background(), ref); code(err) != apperr.InvalidState {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// stalled
	ref := f.single(panetest.ScreenWorking("omp", "1", "x"), []string{"omp"})
	f.env.Clock.Advance(16 * time.Minute)
	if _, err := f.op.Close(context.Background(), ref); code(err) != apperr.InvalidState {
		t.Errorf("stalled: err = %v", err)
	}
	// unknown
	p := f.env.Pane("%0", 100, 0, 0, "何か", nil)
	p.CurrentCmd = "vim"
	f.env.Tmux.SetPanes(p)
	if _, err := f.op.Close(context.Background(), ref); code(err) != apperr.UnsupportedAgent {
		t.Errorf("unknown: err = %v", err)
	}
	if len(f.env.Tmux.Ops()) != 0 {
		t.Fatalf("何も送らない: %v", f.env.Tmux.Ops())
	}
}

// closingAgentTo は closingAgent と同じ手順で、clear の後の画面を final にする。
func (f *fixture) closingAgentTo(final string) {
	var last string
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "keys" && o.Keys[0] == "Enter":
			switch last {
			case "/exit":
				f.becomeShell("終了しました\n" + final)
			case "cd ~", "clear":
				f.becomeShell(final)
			}
		}
	})
}

func readScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // テストの固定のサンプル画面のパス
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(b), "\n")
}

// 枠付き複数行のプロンプト（fish）でも、片付いていれば ok: true・メモを履歴へ移す。
func TestCloseMultilinePromptOK(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
	f.closingAgentTo(readScreen(t, "fish-multiline-prompt.txt"))
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.State != agent.StateShell || !res.NoteArchived {
		t.Fatalf("res = %+v", res)
	}
	if len(f.env.Notes.Archives()) != 1 {
		t.Fatalf("archives = %+v", f.env.Notes.Archives())
	}
}

// 実際のアダプターでも、複数行プロンプトをエージェントの画面と取り違えない。
func TestCloseMultilinePromptRealAdapters(t *testing.T) {
	env := panetest.NewEnv()
	env.Adapters = map[string]agent.Adapter{}
	for name, newAdapter := range map[string]func(agent.Overrides) (agent.Adapter, error){
		"omp": omp.New, "claude": claude.New, "codex": codex.New,
	} {
		ad, err := newAdapter(agent.Overrides{})
		if err != nil {
			t.Fatal(err)
		}
		env.Adapters[name] = ad
	}
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: env.FS,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{env: env, reg: reg, op: ops.New(reg)}
	ref := f.single(panetest.ScreenShell(), nil)
	f.closingAgentTo(readScreen(t, "fish-multiline-prompt.txt"))
	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// プロンプトの前に出力が残っている、枠が閉じていない、~ がない画面は片付いたとみなさない。
func TestCloseNotCleanScreensStayFalse(t *testing.T) {
	for name, screen := range map[string]string{
		"出力が残る":      "エラー: 何かが残っている\n~ $",
		"枠の中に出力が残る":  "╭─  ~ ─── 21:23:48\n何かの出力\n╰─❯",
		"枠が閉じていない":   "╭─  ~ ─── 21:23:48\n何かの出力",
		"~ がない":      "╭─  /tmp ─── 21:23:48\n╰─❯",
		"プロンプトが 4 行": "╭─\n│\n│ ~\n╰─❯",
		"閉じた箱":       "╭──────╮\n│ ~/x  │\n╰──────╯",
		"閉じた箱の途中行":   "╭─  ~\n│ 出力 │\n╰─❯",
		"右が開いた間の行":   "╭─  ~\n│ error: foo\n╰─❯",
		"2 行で末尾が出力":  "╭─ ~/proj\n╰─ done: 3 files changed",
		"~ が末尾行だけ":   "╭─ proj\n╰─❯ ~",
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ref := f.single(panetest.ScreenShell(), nil)
			f.env.Notes.SetSummary("%0", notes.Summary{From: "agent-b"})
			f.closingAgentTo(screen)
			res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
			if err != nil {
				t.Fatal(err)
			}
			if res.OK || res.NoteArchived || len(f.env.Notes.Archives()) != 0 {
				t.Fatalf("res = %+v", res)
			}
		})
	}
}
