package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/allagents"
	"github.com/ousiassllc/swarm-console/test/fakeagent/fakeagenttest"
)

const (
	waitLimit = 20 * time.Second
	pollFirst = 10 * time.Millisecond
	pollMax   = 250 * time.Millisecond
)

var (
	binOnce sync.Once
	binDir  string
	binRoot string // TestMain が消す
)

// sharedBin は擬似エージェントのビルドをテスト全体で 1 回にする。
func sharedBin(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		var err error
		if binRoot, err = os.MkdirTemp("", "fakeagent-bin-"); err != nil {
			t.Fatal(err)
		}
		binDir = fakeagenttest.Install(t, binRoot)
	})
	if binDir == "" {
		t.Fatal("擬似エージェントをビルドできていません")
	}
	return binDir
}

func TestMain(m *testing.M) {
	code := m.Run()
	if binRoot != "" {
		_ = os.RemoveAll(binRoot)
	}
	os.Exit(code)
}

// tmuxSession は専用ソケット（-L）の一時セッション。既定のサーバーには触れない。
type tmuxSession struct {
	t      *testing.T
	socket string
}

func startTmux(t *testing.T, binDir, cmdline string) *tmuxSession {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux が見つかりません。統合テストには tmux が必要です（NFR-TEST-8）: %v", err)
	}
	s := &tmuxSession{t: t, socket: fmt.Sprintf("fakeagent-%d-%d", os.Getpid(), time.Now().UnixNano())}
	shell := fmt.Sprintf("PATH=%s:$PATH exec %s", binDir, cmdline)
	if out, err := s.run("-f", "/dev/null", "new-session", "-d", "-x", "120", "-y", "40", "-s", "t", shell); err != nil {
		t.Fatalf("tmux を起動できません: %v\n%s", err, out)
	}
	sock := strings.TrimSpace(s.must("display-message", "-p", "#{socket_path}"))
	t.Cleanup(func() {
		_, _ = s.run("kill-server")
		_ = os.Remove(sock) // kill-server はソケットのファイルを残すことがある
	})
	return s
}

func (s *tmuxSession) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// -f は new-session ではなく tmux 本体の引数なので、先頭にあれば -L のあとに置く。
	full := []string{"-L", s.socket}
	if len(args) > 1 && args[0] == "-f" {
		full = append(full, args[0], args[1])
		args = args[2:]
	}
	out, err := exec.CommandContext(ctx, "tmux", append(full, args...)...).CombinedOutput() //nolint:gosec // 専用ソケットの tmux へテストが組んだ引数を渡す
	return string(out), err
}

func (s *tmuxSession) must(args ...string) string {
	s.t.Helper()
	out, err := s.run(args...)
	if err != nil {
		s.t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return out
}

// screen は画面を取る。tmux のサーバー（＝擬似エージェントのプロセス）が消えていれば ok=false。
func (s *tmuxSession) screen() (scr agent.Screen, ok bool) {
	out, err := s.run("capture-pane", "-p", "-t", "t")
	return agent.NewScreen(strings.Split(out, "\n")), err == nil
}

// waitFor は cond が真になるまで画面を取り直す。待ちの間隔は短く始めて徐々に伸ばし（高負荷のホストで
// 自分の tmux 呼び出しが負荷を増やさない）、擬似エージェントが先に終わっていたら上限を待たずに失敗する。
// 上限（waitLimit）は「準備完了の READY が出る」ための待ちで、キーは READY を確かめてから送る。
func (s *tmuxSession) waitFor(what string, cond func(agent.Screen) bool) agent.Screen {
	s.t.Helper()
	deadline := time.Now().Add(waitLimit)
	for step := pollFirst; ; step = min(step*2, pollMax) {
		scr, alive := s.screen()
		if !alive {
			s.t.Fatalf("%s を待っている間に擬似エージェントが終わりました", what)
		}
		if cond(scr) {
			return scr
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("%s を待ちましたが現れません。画面:\n%s", what, strings.Join(scr.Lines, "\n"))
		}
		time.Sleep(step)
	}
}

func hasReady(s agent.Screen) bool { return strings.Contains(strings.Join(s.Lines, "\n"), ready) }

func inState(a agent.Adapter, st agent.State) func(agent.Screen) bool {
	return func(s agent.Screen) bool { return hasReady(s) && a.Detect(s).State == st }
}

func cursorAt(a agent.Adapter, n int) func(agent.Screen) bool {
	return func(s agent.Screen) bool {
		c, ok := a.ParseCard(s)
		return hasReady(s) && ok && c.Cursor == n
	}
}

// 起動したプロセスは /proc の argv[0] がアダプタの MatchProcess に当たり、実 tmux の画面が Detect で idle と判定される。
func TestRealTmuxProcessNameAndIdle(t *testing.T) {
	binDir := sharedBin(t)
	for _, name := range fakeagenttest.Names {
		t.Run(name, func(t *testing.T) {
			a := agenttest.New(t, name)
			s := startTmux(t, binDir, name+" idle")
			s.waitFor("READY と idle", inState(a, agent.StateIdle))
			pid := strings.TrimSpace(s.must("display-message", "-p", "-t", "t", "#{pane_pid}"))
			raw, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline")) //nolint:gosec // tmux が答えたペインの pid の cmdline を読む
			if err != nil {
				t.Fatal(err)
			}
			argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
			if !a.MatchProcess(argv) {
				t.Errorf("argv %q が %s の MatchProcess に当たらない", argv, name)
			}
			for _, other := range fakeagenttest.Names {
				if other != name && agenttest.New(t, other).MatchProcess(argv) {
					t.Errorf("argv %q が %s にも当たる", argv, other)
				}
			}
			s.must("send-keys", "-t", "t", "-l", a.ExitCommand())
			s.must("send-keys", "-t", "t", "Enter")
			waitGone(t, s)
		})
	}
}

func waitGone(t *testing.T, s *tmuxSession) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for {
		if _, err := s.run("has-session", "-t", "t"); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("終了コマンドのあとプロセスが終わりません")
		}
		time.Sleep(pollFirst)
	}
}

// ↑/↓ で画面のカーソルが動き、Enter で選んだ番号が記録されて working になる。
func TestRealTmuxAskPick(t *testing.T) {
	binDir := sharedBin(t)
	cases := []struct{ agent, scenario string }{{"omp", "ask"}, {"claude", "ask"}, {"claude", "ask-custom"}, {"codex", "ask"}}
	for _, c := range cases {
		t.Run(c.agent+"/"+c.scenario, func(t *testing.T) {
			a, logPath := agenttest.New(t, c.agent), filepath.Join(t.TempDir(), "log")
			s := startTmux(t, binDir, fmt.Sprintf("%s %s --tick 50ms --log %s", c.agent, c.scenario, logPath))
			scr := s.waitFor("asking", inState(a, agent.StateAsking))
			card, _ := a.ParseCard(scr)
			keys, err := a.PickKeys(card, 3)
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range keys {
				s.must("send-keys", "-t", "t", k.Name)
			}
			// 数字キーも Up/Down も、カーソルを動かしてから Enter で確定する。
			s.waitFor("カーソルが 3 番へ", cursorAt(a, 3))
			s.must("send-keys", "-t", "t", "Enter")
			s.waitFor("working", inState(a, agent.StateWorking))
			assertEvents(t, logPath, fakeagenttest.Event{Kind: "PICK", N: 3})
			s.must("send-keys", "-t", "t", "Escape")
			s.waitFor("Escape で idle", inState(a, agent.StateIdle))
		})
	}
}

func TestRealTmuxCustomAnswer(t *testing.T) {
	binDir := sharedBin(t)
	for _, c := range []struct{ agent, scenario string }{{"omp", "ask-custom"}, {"claude", "ask-custom"}} {
		t.Run(c.agent, func(t *testing.T) {
			a, logPath := agenttest.New(t, c.agent), filepath.Join(t.TempDir(), "log")
			s := startTmux(t, binDir, fmt.Sprintf("%s %s --tick 50ms --log %s", c.agent, c.scenario, logPath))
			scr := s.waitFor("asking", inState(a, agent.StateAsking))
			card, _ := a.ParseCard(scr)
			keys, err := a.CustomKeys(card)
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range keys {
				s.must("send-keys", "-t", "t", k.Name)
			}
			s.waitFor("カーソルが「その他」へ", cursorAt(a, len(card.Options)+1))
			if c.agent == "omp" { // omp は Enter で自由入力の枠が開く。claude は「その他」の行へそのまま入る。
				s.must("send-keys", "-t", "t", "Enter")
				s.waitFor("自由入力の枠", func(sc agent.Screen) bool {
					return hasReady(sc) && strings.Contains(strings.Join(sc.Lines, "\n"), "Custom answer")
				})
			}
			s.must("send-keys", "-t", "t", "-l", "Mango")
			s.must("send-keys", "-t", "t", "Enter")
			s.waitFor("working", inState(a, agent.StateWorking))
			assertEvents(t, logPath, fakeagenttest.Event{Kind: "CUSTOM", Text: "Mango"})
		})
	}
}

// paste-buffer -p の貼り付けは bracketed paste として届き、本文が画面に出ずログに残る。Enter で受理されて working になる。
func TestRealTmuxBracketedPaste(t *testing.T) {
	binDir := sharedBin(t)
	const body = "## やること\n\n日本語 line2\n\ttab"
	a, logPath := agenttest.New(t, "omp"), filepath.Join(t.TempDir(), "log")
	s := startTmux(t, binDir, fmt.Sprintf("omp idle --tick 50ms --log %s", logPath))
	before := s.waitFor("idle", inState(a, agent.StateIdle))
	load := exec.CommandContext(context.Background(), "tmux", "-L", s.socket, "load-buffer", "-b", "sc", "-") //nolint:gosec // 専用ソケットの tmux へ固定の引数を渡す
	load.Stdin = bytes.NewBufferString(body)
	if out, err := load.CombinedOutput(); err != nil {
		t.Fatalf("load-buffer: %v\n%s", err, out)
	}
	s.must("paste-buffer", "-p", "-d", "-b", "sc", "-t", "t")
	s.must("send-keys", "-t", "t", "Enter")
	after := s.waitFor("受理されて working", inState(a, agent.StateWorking))
	assertEvents(t, logPath, fakeagenttest.Event{Kind: "PASTE", Text: body}, fakeagenttest.Event{Kind: "SUBMIT"})
	if strings.Contains(strings.Join(after.Lines, "\n"), "日本語") || strings.Contains(strings.Join(before.Lines, "\n"), "日本語") {
		t.Error("貼り付けた本文が画面に反映されている")
	}
}

func assertEvents(t *testing.T, path string, want ...fakeagenttest.Event) {
	t.Helper()
	got, err := fakeagenttest.ReadLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ログ = %+v, want %+v", got, want)
	}
}
