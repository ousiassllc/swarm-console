// Package nativetest は、omp が tern の画面（TSP）を出しているペインと、普通の画面のペインを、本物の omp アダプタ・Registry・Ops・Notifier で
// 同じように扱えることを確かめるテスト（Issue #121）。tmux・/proc はフェイク。tern の画面の omp は、実機（omp v19.0.1、tern 0.9.29）で取った
// セッションの記録（testdata/native/omp/session.jsonl）を、テストが開いたままのファイルとして読ませる。
package nativetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/omp" // アダプタの登録
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

const (
	nativeID  = "%0" // tern の画面（TSP）の omp
	classicID = "%1" // 普通の画面の omp
)

// rig は 2 つのペインを持つ。%0 は capture-pane が空でタイトルが TSP のフレーム、会話の記録はテストが書く。%1 は普通の画面（SetClassic で差し替える）。
type rig struct {
	t       *testing.T
	env     *panetest.Env
	reg     *pane.SnapshotRegistry
	op      *ops.Operator
	un      *pane.Unaccepted
	session *os.File // %0 の omp が開いている会話の記録
	fixture []string
	lines   int // fixture のうち会話の記録に書いた行数
}

// newRig は fixture の先頭 n 行を会話の記録に書いた状態の rig を作る。omp のプロセスは、このテスト自身（pid）の /proc/<pid>/fd を使う。
func newRig(t *testing.T, n int, observer pane.SnapshotObserver, fs pane.ReportFS, un *pane.Unaccepted) *rig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".omp", "agent", "sessions", "--work--")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "2026-11-20T13-45-43-697Z_x.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600) //nolint:gosec // テストの一時ディレクトリ
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	ad, err := agent.New("omp", agent.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	env := panetest.NewEnv()
	env.Adapters = map[string]agent.Adapter{"omp": ad}
	if un == nil {
		un = pane.NewUnaccepted()
	}
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: fs,
		Unaccepted: un, Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	g := &rig{t: t, env: env, reg: reg, op: ops.New(reg), un: un, session: f, fixture: fixtureLines(t)}
	// %0 のシェルの pid は自分の pid - 1。FakeHost はプロセス木を shell, omp の順に pid, pid+1 で作る。
	shell := os.Getpid() - 1
	native := env.Pane(nativeID, shell, 0, 0, "", []string{"omp"})
	native.Title = panetest.NativeFrame
	classic := env.Pane(classicID, 500, 20, 0, "", []string{"omp"})
	classic.Title = "π > work"
	env.Tmux.SetPanes(native, classic)
	g.appendFixture(n)
	return g
}

func fixtureLines(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agenttest.Root(t), "testdata", "native", "omp", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// appendFixture は fixture の n 行目まで（すでに書いた分は除く）を会話の記録に追記する。omp が 1 つずつ追記するのと同じ。
func (g *rig) appendFixture(n int) {
	g.t.Helper()
	for ; g.lines < n; g.lines++ {
		g.write(g.fixture[g.lines])
	}
}

func (g *rig) write(line string) {
	g.t.Helper()
	if _, err := g.session.WriteString(line + "\n"); err != nil {
		g.t.Fatal(err)
	}
}

// userSays は、いまの時計の時刻に text が指示として記録されたことにする（omp は Enter で送信されると、その指示を記録する）。
func (g *rig) userSays(text string) {
	g.t.Helper()
	g.write(fmt.Sprintf(`{"type":"message","id":"u","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":%q}]}}`,
		g.env.Clock.Now().Format(time.RFC3339Nano), text))
}

// assistantStops は、いまの時計の時刻に最終の応答（番の終わり）が記録されたことにする。
func (g *rig) assistantStops() {
	g.t.Helper()
	g.write(fmt.Sprintf(`{"type":"message","id":"a","timestamp":%q,"message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"done"}]}}`,
		g.env.Clock.Now().Format(time.RFC3339Nano)))
}

func (g *rig) setClassic(screen string) { g.env.Tmux.SetScreen(classicID, screen) }

// classicSample は testdata/screens/omp の実画面。
func classicSample(t *testing.T, file string) string {
	t.Helper()
	return strings.Join(agent.SampleLines(agenttest.Text(t, "omp", file)), "\n")
}

// classicFixture は testdata/native/omp の、PI_TUI_NATIVE=0 で取った実画面。
func classicFixture(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agenttest.Root(t), "testdata", "native", "omp", file)) //nolint:gosec // テストの固定の fixture
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(agent.SampleLines(string(b)), "\n")
}

func (g *rig) states() (native, classic pane.PaneState) {
	g.t.Helper()
	snap, err := g.reg.Refresh(context.Background())
	if err != nil {
		g.t.Fatal(err)
	}
	n, ok1 := snap.Find(nativeID)
	c, ok2 := snap.Find(classicID)
	if !ok1 || !ok2 {
		g.t.Fatalf("ペインが無い: %+v", snap.Panes)
	}
	return *n, *c
}
