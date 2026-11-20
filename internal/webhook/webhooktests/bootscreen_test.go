package webhooktests

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// Issue #113: omp が負荷で画面を描き直せず、作業中なのに画面が idle（起動画面）のままでも、
// ログの作業の跡で working と読み、作業が終われば pane.idle_report が 1 回だけ届く。
// 実際の SnapshotRegistry（Observer は Notifier）に、ログの作業の跡を与えるフェイクのアダプタを載せて確かめる。

type frozenScreen struct {
	*rig
	env   *panetest.Env
	probe *panetest.ProbeAdapter
	un    *pane.Unaccepted
	reg   *pane.SnapshotRegistry
}

const frozenPID = 101 // env.Pane("%0", 100, ...) の omp のプロセス

func newFrozenScreen(t *testing.T, r *receiver) *frozenScreen {
	t.Helper()
	g := newRig(t, r.target(3))
	env := panetest.NewEnv()
	env.Cfg.ReportDir = filepath.Join(g.dir, "reports")
	probe := panetest.NewProbeAdapter("omp")
	env.Adapters["omp"] = probe
	env.Tmux.SetPanes(env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"})) // 起動画面のまま
	un := pane.NewUnaccepted()
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes,
		Clock: env.Clock, Unaccepted: un, Observer: g.n,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &frozenScreen{rig: g, env: env, probe: probe, un: un, reg: reg}
}

// see は時計を d 進めてスナップショットを作り、ペインの状態を返す（Observer の判定は取得の完了前に済む）。
func (f *frozenScreen) see(d time.Duration) agent.State {
	f.t.Helper()
	f.env.Clock.Advance(d)
	snap, err := f.reg.Refresh(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return snap.Panes[0].State
}

func (f *frozenScreen) writeReport() { f.writeReportBody("done\n") }

// writeReportBody は最終報告を body で（書き直して）置く。body の長さが変わると「更新された」と見える。
func (f *frozenScreen) writeReportBody(body string) {
	f.t.Helper()
	dir := filepath.Join(f.dir, "reports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "T1.md"), []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func TestFrozenScreenWorkPostsIdleReportOnce(t *testing.T) {
	r := newReceiver(t, "KEY-F-0000", "TOK-F-0000")
	f := newFrozenScreen(t, r)

	f.see(time.Second)
	f.un.Mark("%0", f.env.Clock.Now()) // dispatch が Enter の受理を確かめられなかった

	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	if got := f.see(5 * time.Second); got != agent.StateWorking {
		t.Fatalf("起動画面のまま作業しているのに %s", got)
	}
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	f.see(30 * time.Second)
	f.writeReport() // 作業の終わりに最終報告を書く
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	if got := f.see(5 * time.Second); got != agent.StateWorking {
		t.Fatalf("作業の途中なのに %s", got)
	}
	if n := len(r.received()); n != 0 {
		t.Fatalf("作業の途中で送った: %d 回", n)
	}

	// ログの作業の跡が 2 分途切れたら idle になり、その回の pane.idle_report が 1 回だけ届く。
	if got := f.see(3 * time.Minute); got != agent.StateIdle {
		t.Fatalf("作業の跡が途切れたのに %s", got)
	}
	for range 5 {
		f.see(5 * time.Second)
	}
	f.drain()

	hits := r.received()
	if len(hits) != 1 {
		t.Fatalf("POST = %d 回, want 1\n%s", len(hits), f.logText())
	}
	if pl := payloadOf(t, hits[0]); pl.Event != webhook.EventPaneIdleReport || pl.PaneID != "%0" || pl.Pane != "T1" {
		t.Errorf("payload = %+v", pl)
	}
}

// 受理を確かめられず、実際にも止まっていた（ログに作業の跡が無い）ペインは working にならず、何も送らない。
func TestFrozenScreenThatNeverWorkedPostsNothing(t *testing.T) {
	r := newReceiver(t, "KEY-F-0000", "TOK-F-0000")
	f := newFrozenScreen(t, r)

	f.see(time.Second)
	f.un.Mark("%0", f.env.Clock.Now())
	f.writeReport() // 前の作業の最終報告が置かれたままでも
	for range 20 {
		if got := f.see(time.Minute); got != agent.StateIdle {
			t.Fatalf("止まっているのに %s", got)
		}
	}
	f.drain()
	if n := len(r.received()); n != 0 {
		t.Fatalf("止まっているペインで %d 回送った", n)
	}
}
