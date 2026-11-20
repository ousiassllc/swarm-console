package registrytest

import (
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// Issue #113: 指示の Enter を送ったのに受理を画面で確かめられなかったペイン（Unaccepted）は、画面が idle のままでも、
// エージェントのログに作業の跡があれば working と読む。omp は負荷が高いと画面の描き直しが止まるため。

const logPID = 101 // env.Pane("%0", 100, ...) の omp のプロセス（シェルが 100、その子が 101）

type logRig struct {
	env   *panetest.Env
	probe *panetest.ProbeAdapter
	un    *pane.Unaccepted
	reg   *pane.SnapshotRegistry
}

func newLogRig(t *testing.T) *logRig {
	t.Helper()
	env := panetest.NewEnv()
	probe := panetest.NewProbeAdapter("omp")
	env.Adapters["omp"] = probe
	env.Tmux.SetPanes(env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}))
	un := pane.NewUnaccepted()
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: env.FS, Unaccepted: un,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &logRig{env: env, probe: probe, un: un, reg: reg}
}

func (g *logRig) state(t *testing.T) agent.State {
	t.Helper()
	return byID(t, mustRefresh(t, g.reg), "%0").State
}

// 受理を確かめられなかったペインは、ログに作業の跡があれば working、その跡が途切れて 2 分たてば idle、また書かれれば working。
func TestUnacceptedPaneIsWorkingWhileLogShowsWork(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("ログに作業の跡が無い間は idle のまま: %s", got)
	}

	g.env.Clock.Advance(30 * time.Second)
	g.probe.AddWork(logPID, g.env.Clock.Now())
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("ログに作業の跡: %s, want working", got)
	}

	g.env.Clock.Advance(119 * time.Second)
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("最後の作業の行から 119 秒: %s, want working", got)
	}
	g.env.Clock.Advance(2 * time.Second)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("最後の作業の行から 2 分を過ぎた: %s, want idle", got)
	}

	g.probe.AddWork(logPID, g.env.Clock.Now())
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("作業の行がまた書かれた: %s, want working", got)
	}
}

// 記録の無いペインは、ログに作業の跡があっても画面どおり（普通の idle の動きは変わらない）。
func TestPaneWithoutUnacceptedMarkIgnoresLog(t *testing.T) {
	g := newLogRig(t)
	g.probe.AddWork(logPID, g.env.Clock.Now())
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("記録が無いのにログで working にした: %s", got)
	}
}

// 受理を確かめられず、ログにも作業の跡が無い（実際に止まっている）ペインは working にしない。10 分たてば記録も捨てる。
func TestUnacceptedPaneThatNeverWorkedStaysIdleAndIsForgotten(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	g.env.Clock.Advance(9 * time.Minute)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("止まっているのに %s", got)
	}
	if _, ok := g.un.Since("%0"); !ok {
		t.Fatal("9 分では記録を捨てない")
	}
	g.env.Clock.Advance(2 * time.Minute)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("止まっているのに %s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("10 分たっても作業の跡が無いのに記録が残っている")
	}
	g.probe.AddWork(logPID, g.env.Clock.Now()) // 捨てたあとにログが書かれても、画面どおり
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("記録を捨てたあとに working になった: %s", got)
	}
}

// Enter より前のログの行（起動中の行など）は数えない。
func TestWorkBeforeTheEnterIsNotCounted(t *testing.T) {
	g := newLogRig(t)
	g.probe.AddWork(logPID, g.env.Clock.Now())
	g.env.Clock.Advance(time.Second)
	g.un.Mark("%0", g.env.Clock.Now())
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("Enter より前の行で working にした: %s", got)
	}
}

// 画面が working と読めるようになったら（画面が回復した）、記録を捨てる。
func TestRecoveredScreenClearsUnacceptedMark(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	g.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "a", "build"))
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("%s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("画面が working になったのに記録が残っている")
	}
}
