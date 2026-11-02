package registrytest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

func TestSnapshotStates(t *testing.T) {
	env := panetest.NewEnv()
	shell := env.Pane("%3", 103, 20, 50, panetest.ScreenShell(), nil)
	vim := env.Pane("%4", 104, 40, 0, "~\n~\n", nil)
	vim.CurrentCmd = "vim"
	env.Host.SetTree(104, []string{"bash"}, []string{"vim"})
	ps := []tmux.PaneInfo{
		env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}),
		env.Pane("%1", 101, 0, 50, panetest.ScreenWorking("omp", "1", "Running go test")+"\nEL: 42m\n", []string{"/usr/bin/omp"}),
		env.Pane("%2", 102, 20, 0, panetest.ScreenAsk("claude", "どちらにしますか？", 1, true, "A", "B"), []string{"claude", "--x"}),
		shell, vim,
	}
	env.Tmux.SetPanes(ps...)
	snap := mustRefresh(t, newTestRegistry(t, env))

	type expect struct {
		agent string
		state agent.State
		pos   string
	}
	// 3 段（top 0・20・40）なので位置は R<段><列>。
	want := map[string]expect{
		"%0": {"omp", agent.StateIdle, "R11"},
		"%1": {"omp", agent.StateWorking, "R12"},
		"%2": {"claude", agent.StateAsking, "R21"},
		"%3": {"", agent.StateShell, "R22"},
		"%4": {"", agent.StateUnknown, "R31"},
	}
	for id, w := range want {
		p := byID(t, snap, id)
		if p.Agent != w.agent || p.State != w.state || p.Pos != w.pos {
			t.Errorf("%s: agent=%q state=%s pos=%s, want %+v", id, p.Agent, p.State, p.Pos, w)
		}
	}
	if p := byID(t, snap, "%1"); p.Activity != "Running go test" || p.Elapsed != "42m" {
		t.Errorf("working: activity=%q elapsed=%q", p.Activity, p.Elapsed)
	}
	if p := byID(t, snap, "%2"); p.Activity != "どちらにしますか？" {
		t.Errorf("asking の Activity は質問文のはず: %q", p.Activity)
	}
	if p := byID(t, snap, "%3"); p.Activity != "" || p.Elapsed != "" {
		t.Errorf("shell: %+v", p)
	}
	if !snap.TmuxSession || len(snap.Panes) != 5 || snap.At != panetest.T0 {
		t.Errorf("snapshot: %+v", snap)
	}
	if got := []string{snap.Panes[0].ID, snap.Panes[1].ID, snap.Panes[2].ID, snap.Panes[3].ID, snap.Panes[4].ID}; !reflect.DeepEqual(got, []string{"%0", "%1", "%2", "%3", "%4"}) {
		t.Errorf("位置順になっていない: %v", got)
	}
}

func TestIdentifyProcessBeforeScreen(t *testing.T) {
	env := panetest.NewEnv()
	// 画面は omp の目印だが、プロセス木は claude → プロセスが優先。
	p1 := env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"claude"})
	// プロセス木が読めない（SetTree なし）→ 画面の特徴で補う。
	p2 := tmux.PaneInfo{ID: "%1", Session: "0", PaneIndex: 1, Left: 50, PID: 999, CurrentCmd: "node"}
	env.Tmux.SetScreen("%1", panetest.ScreenIdle("omp"))
	// プロセス木が読めて前面がシェル・agent なし → 画面に過去の omp が残っていても shell。
	p3 := env.Pane("%2", 102, 20, 0, panetest.ScreenIdle("omp"), nil)
	// プロセス木が読めなくて前面がシェル → 画面に過去の omp が残っていても shell（Issue #35）。
	p4 := tmux.PaneInfo{ID: "%3", Session: "0", PaneIndex: 3, Left: 70, PID: 998, CurrentCmd: "bash"}
	env.Tmux.SetScreen("%3", panetest.ScreenIdle("omp"))
	env.Tmux.SetPanes(p1, p2, p3, p4)
	snap := mustRefresh(t, newTestRegistry(t, env))
	if a := byID(t, snap, "%0").Agent; a != "claude" {
		t.Errorf("プロセス優先: %q", a)
	}
	if p := byID(t, snap, "%1"); p.Agent != "omp" || p.State != agent.StateIdle {
		t.Errorf("画面で補う: %+v", p)
	}
	if p := byID(t, snap, "%2"); p.Agent != "" || p.State != agent.StateShell {
		t.Errorf("シェルは画面の残りで誤判定しない: %+v", p)
	}
	if p := byID(t, snap, "%3"); p.Agent != "" || p.State != agent.StateShell {
		t.Errorf("/proc が読めなくても前面がシェルなら shell: %+v", p)
	}
}

func TestStalledAndChangedAt(t *testing.T) {
	env := panetest.NewEnv()
	p := env.Pane("%0", 100, 0, 0, panetest.ScreenWorking("omp", "a", "build"), []string{"omp"})
	env.Tmux.SetPanes(p)
	r := newTestRegistry(t, env)

	s := mustRefresh(t, r)
	first := byID(t, s, "%0")
	if first.State != agent.StateWorking || !first.ChangedAt.Equal(panetest.T0) || first.ChangedAgo != "0s" {
		t.Fatalf("初回は起動時刻: %+v", first)
	}
	// スピナーだけ変わっても ChangedAt は動かない。起動後 14 分ではまだ stalled にならない。
	env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "b", "build"))
	env.Clock.Advance(14 * time.Minute)
	got := byID(t, mustRefresh(t, r), "%0")
	if got.State != agent.StateWorking || got.ScreenHash != first.ScreenHash || got.ChangedAgo != "14m" || got.Elapsed != "14m" {
		t.Fatalf("14 分: %+v", got)
	}
	// 15 分経つと stalled。
	env.Clock.Advance(time.Minute)
	if got := byID(t, mustRefresh(t, r), "%0"); got.State != agent.StateStalled || got.ChangedAgo != "15m" {
		t.Fatalf("15 分: %+v", got)
	}
	// 本文が変われば ChangedAt が更新され working に戻る。
	env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "c", "test"))
	env.Clock.Advance(time.Second)
	got = byID(t, mustRefresh(t, r), "%0")
	if got.State != agent.StateWorking || !got.ChangedAt.Equal(env.Clock.Now()) || got.ChangedAgo != "0s" || got.ScreenHash == first.ScreenHash {
		t.Fatalf("変化後: %+v", got)
	}
	// working でなくなれば stalled にならず、elapsed も消える。
	env.Tmux.SetScreen("%0", panetest.ScreenIdle("omp"))
	env.Clock.Advance(time.Hour)
	if got := byID(t, mustRefresh(t, r), "%0"); got.State != agent.StateIdle || got.Elapsed != "" {
		t.Fatalf("idle: %+v", got)
	}
}

func TestNewPaneAfterStartupChangedAtIsNow(t *testing.T) {
	env := panetest.NewEnv()
	env.Tmux.SetPanes(env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}))
	r := newTestRegistry(t, env)
	mustRefresh(t, r)
	env.Clock.Advance(time.Hour)
	env.Tmux.SetPanes(
		env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}),
		env.Pane("%1", 101, 0, 50, panetest.ScreenWorking("omp", "a", "x"), []string{"omp"}),
	)
	s := mustRefresh(t, r)
	if p := byID(t, s, "%1"); !p.ChangedAt.Equal(env.Clock.Now()) || p.State != agent.StateWorking {
		t.Fatalf("後から増えたペインは now: %+v", p)
	}
	if p := byID(t, s, "%0"); p.ChangedAgo != "1h00m" {
		t.Fatalf("changed_ago: %q", p.ChangedAgo)
	}
}

func TestElapsedFromObservationAndActivityTruncation(t *testing.T) {
	env := panetest.NewEnv()
	long := strings.Repeat("あ", 130)
	env.Tmux.SetPanes(env.Pane("%0", 100, 0, 0, panetest.ScreenWorking("omp", "a", long), []string{"omp"}))
	r := newTestRegistry(t, env)
	p := byID(t, mustRefresh(t, r), "%0")
	if n := len([]rune(p.Activity)); n != 120 || !p.ActivityTrunc || p.Elapsed != "0s" {
		t.Fatalf("activity=%d trunc=%v elapsed=%q", n, p.ActivityTrunc, p.Elapsed)
	}
	env.Clock.Advance(3 * time.Minute)
	env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "b", "short"))
	p = byID(t, mustRefresh(t, r), "%0")
	if p.Elapsed != "3m" || p.Activity != "short" || p.ActivityTrunc {
		t.Fatalf("観測開始からの経過: %+v", p)
	}
}

func TestNoteAndReportExist(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	env.Notes.SetSummary("%1", notes.Summary{From: "agent-b", Items: []string{"#1"}})
	env.FS.Put("/reports/T1.md", "")
	s := mustRefresh(t, newTestRegistry(t, env))
	if n := byID(t, s, "%1").Note; n == nil || n.From != "agent-b" {
		t.Fatalf("note: %+v", n)
	}
	if byID(t, s, "%0").Note != nil {
		t.Fatal("メモが無いペインは nil")
	}
	if !byID(t, s, "%0").ReportExists || byID(t, s, "%1").ReportExists {
		t.Fatal("report_exists")
	}
}

func TestPaneUnreadableDoesNotStopOthers(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	env.Tmux.SetCaptureErr("%1", errBoom)
	s := mustRefresh(t, newTestRegistry(t, env))
	if p := byID(t, s, "%1"); p.State != agent.StateUnknown || p.Agent != "" {
		t.Fatalf("読めないペイン: %+v", p)
	}
	if p := byID(t, s, "%0"); p.State != agent.StateIdle {
		t.Fatalf("他のペインは読める: %+v", p)
	}
	if codes := warningCodes(s); !reflect.DeepEqual(codes, []string{pane.WarnPaneUnreadable}) {
		t.Fatalf("warnings = %v", codes)
	}
	if !strings.Contains(s.Warnings[0].Message, "T2") {
		t.Fatalf("message = %q", s.Warnings[0].Message)
	}
}

func TestTmuxUnavailableAndSessionMissing(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	mustRefresh(t, r)

	env.Tmux.SetListErr(errBoom)
	s := mustRefresh(t, r)
	if len(s.Panes) != 0 || s.TmuxSession || !reflect.DeepEqual(warningCodes(s), []string{pane.WarnTmuxUnavailable}) {
		t.Fatalf("tmux 不通: %+v", s)
	}
	// 不通のあと戻っても、並び変更の警告は出さず、メモも動かさない。
	env.Tmux.SetListErr(nil)
	s = mustRefresh(t, r)
	if len(s.Panes) != 3 || len(s.Warnings) != 0 || len(env.Notes.Archives()) != 0 {
		t.Fatalf("復旧: %+v archives=%v", s, env.Notes.Archives())
	}
	// セッションが無い（一覧が空）: 落ちずに空＋session_missing。メモも履歴へ移さない。
	env.Tmux.SetPanes()
	for range 3 {
		s = mustRefresh(t, r)
	}
	if len(s.Panes) != 0 || s.TmuxSession || !reflect.DeepEqual(warningCodes(s), []string{pane.WarnSessionMissing}) || s.Layout.Cols == nil {
		t.Fatalf("session_missing: %+v", s)
	}
	if len(env.Notes.Archives()) != 0 {
		t.Fatalf("空の一覧で履歴へ移した: %v", env.Notes.Archives())
	}
}

func TestSessionMissingWhenOnlySomeSessionsExist(t *testing.T) {
	env := panetest.NewEnv()
	env.Cfg.Sessions = []string{"0", "gone"}
	twoRows(env)
	s := mustRefresh(t, newTestRegistry(t, env))
	if !s.TmuxSession || len(s.Panes) != 3 || !reflect.DeepEqual(warningCodes(s), []string{pane.WarnSessionMissing}) {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(s.Warnings[0].Message, "gone") {
		t.Fatalf("message = %q", s.Warnings[0].Message)
	}
}

// P-30: 最終報告がシンボリックリンクなら report_exists は false（通常ファイルは true）。
func TestReportExistsIgnoresSymlink(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	dir := t.TempDir()
	env.Cfg.ReportDir = dir
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "T1.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "T2.md"), []byte("報告"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: pane.OSReportFS{},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := mustRefresh(t, r)
	if byID(t, s, "%0").ReportExists || !byID(t, s, "%1").ReportExists {
		t.Fatalf("report_exists: リンク(T1)=%v 通常(T2)=%v", byID(t, s, "%0").ReportExists, byID(t, s, "%1").ReportExists)
	}
}
