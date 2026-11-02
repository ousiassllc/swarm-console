package panetest

import (
	"runtime"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// ReportDir はテスト用の最終報告ディレクトリ。
const ReportDir = "/reports"

// Env はフェイク一式と設定。テストが pane.Deps に詰める。
type Env struct {
	Cfg      *config.Config
	Tmux     *FakeTmux
	Host     *FakeHost
	Notes    *FakeNotes
	FS       *MemFS
	Clock    *clock.Fake
	Adapters map[string]agent.Adapter
}

// T0 は Fake 時計の開始時刻。
var T0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// NewEnv は既定の設定（セッション "0"・既定値の期間）で Env を作る。アダプタは omp・claude。
func NewEnv() *Env {
	return &Env{
		Cfg: &config.Config{
			Sessions: []string{"0"}, DefaultSession: "0",
			PollInterval: 5 * time.Second, SnapshotTTL: 2 * time.Second, StalledAfter: 15 * time.Minute,
			LayoutWarningTTL: 10 * time.Minute, AcceptTimeout: 10 * time.Second, StartTimeout: 30 * time.Second,
			LockTimeout: 60 * time.Second, TitlePrefix: "agent: ", ReportDir: ReportDir,
		},
		Tmux: NewTmux(), Host: NewHost(), Notes: NewNotes(), FS: NewMemFS(), Clock: clock.NewFake(T0),
		Adapters: map[string]agent.Adapter{"omp": FakeAdapter{"omp"}, "claude": FakeAdapter{"claude"}},
	}
}

// Pane は 1 ペインを tmux・host に登録する。argv が nil ならプロセス木は shell だけ（エージェントなし）。
// プロセス木が読めない状態にするには、あとで Host.ClearTree(pid) を呼ぶ。
// session "0"・window 0 で、シェルの前面コマンドは bash。
func (e *Env) Pane(id string, pid, top, left int, screen string, argv []string) tmux.PaneInfo {
	p := tmux.PaneInfo{ID: id, Session: "0", PaneIndex: pid, Top: top, Left: left, PID: pid, CurrentCmd: "bash", Title: "t" + id, CurrentPath: "/work"}
	e.Tmux.SetScreen(id, screen)
	if argv != nil {
		e.Host.SetTree(pid, []string{"bash"}, argv)
	} else {
		e.Host.SetTree(pid, []string{"bash"})
	}
	return p
}

// WaitUntil は cond が true になるまで、上限（5 秒）つきで条件を確かめる。固定の待ちではなく、他の goroutine の進行を待つためのもの。
func WaitUntil(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("条件を満たしませんでした: %s", what)
		}
		runtime.Gosched()
	}
}
