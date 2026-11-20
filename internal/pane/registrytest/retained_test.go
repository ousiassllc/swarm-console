package registrytest

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// Issue #115（P-107）: 件を送った（Retain した）ペインは、受理を確かめられなかった指示の 10 分の上限で見張りを捨てず、ログを見続ける。
// 見張りをやめるのは、画面が working・asking になったとき、shell に戻ったとき、ペインが消えたとき、次の指示が受理されたとき。

// countingProbe はログを読んだ回数を数える。
type countingProbe struct {
	*panetest.ProbeAdapter
	reads atomic.Int64
}

func (c *countingProbe) LastWork(pid int, since time.Time) (time.Time, bool) {
	c.reads.Add(1)
	return c.ProbeAdapter.LastWork(pid, since)
}

// retainedRig は %0 を受理されなかったペインにし、作業のあとログが途切れて idle になった（件を送った）ところまで進める。
func retainedRig(t *testing.T) (*logRig, *countingProbe) {
	t.Helper()
	g := newLogRig(t)
	cp := &countingProbe{ProbeAdapter: g.probe}
	g.env.Adapters["omp"] = cp
	g.un.Mark("%0", g.env.Clock.Now())
	g.env.Clock.Advance(time.Second)
	g.probe.AddWork(logPID, g.env.Clock.Now())
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("%s", got)
	}
	g.env.Clock.Advance(3 * time.Minute)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("%s", got)
	}
	g.un.Retain("%0") // webhook が pane.idle_report の件を作った
	return g, cp
}

// idleFor は画面もログも動かないまま d 進める（5 秒おきにスナップショット）。idle 以外になったら失敗する。
func (g *logRig) idleFor(t *testing.T, d time.Duration) {
	t.Helper()
	for waited := time.Duration(0); waited < d; waited += 5 * time.Second {
		g.env.Clock.Advance(5 * time.Second)
		if got := g.state(t); got != agent.StateIdle {
			t.Fatalf("止まっているのに %s", got)
		}
	}
}

// 受け入れ条件 1 の土台: Retain したペインは 10 分を越えて待っても見張りを捨てず、ログに作業の跡が戻れば working と読む。
func TestRetainedPaneStaysWatchedPastTenMinutes(t *testing.T) {
	g, _ := retainedRig(t)
	g.idleFor(t, 30*time.Minute)
	if _, ok := g.un.Since("%0"); !ok {
		t.Fatal("30 分たって見張りを捨てた")
	}
	g.probe.AddWork(logPID, g.env.Clock.Now())
	g.env.Clock.Advance(30 * time.Second) // ログを読む間隔
	// 画面が 15 分以上変わっていないので、ログで読んだ working は stalled になる。
	if got := g.state(t); got != agent.StateWorking && got != agent.StateStalled {
		t.Fatalf("作業の跡が戻ったのに %s", got)
	}
}

// Retain していないペインは今までどおり、作業の跡が無いまま 10 分たてば捨てる（#113）。
func TestUnretainedPaneStillForgottenAfterTenMinutes(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	g.idleFor(t, 11*time.Minute)
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("10 分たっても作業の跡が無いのに記録が残っている")
	}
}

// 記録の無いペイン（受理を確かめられたペイン）は、Retain しても何も見ない。
func TestRetainWithoutMarkDoesNothing(t *testing.T) {
	g := newLogRig(t)
	g.un.Retain("%0")
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("記録の無いペインを見張りにした")
	}
	g.probe.AddWork(logPID, g.env.Clock.Now())
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("%s", got)
	}
}

// ログを読む間隔の上限: Retain したペインは 30 秒に 1 回だけ読む。Retain していないペインは今までどおり、スナップショットごとに読む。
func TestRetainedPaneReadsLogAtMostEveryThirtySeconds(t *testing.T) {
	g, cp := retainedRig(t)
	before := cp.reads.Load()
	g.idleFor(t, 5*time.Minute) // 5 秒おきに 60 回のスナップショット
	if got := cp.reads.Load() - before; got < 9 || got > 11 {
		t.Fatalf("5 分で %d 回読んだ（30 秒おきなら 10 回ほど）", got)
	}

	g2 := newLogRig(t)
	cp2 := &countingProbe{ProbeAdapter: g2.probe}
	g2.env.Adapters["omp"] = cp2
	g2.un.Mark("%0", g2.env.Clock.Now())
	g2.idleFor(t, 2*time.Minute)
	if got := cp2.reads.Load(); got != 24 {
		t.Fatalf("Retain していないペインは毎回読む: %d 回, want 24", got)
	}
}

// 見張るペイン数の上限: 17 個目を Retain したら、Retain が最も古いペインから見るのをやめる。
func TestRetainedPanesAreCapped(t *testing.T) {
	un := pane.NewUnaccepted()
	now := time.Now()
	for i := range 17 {
		id := fmt.Sprintf("%%%d", i)
		un.Mark(id, now)
		un.Retain(id)
	}
	if _, ok := un.Since("%0"); ok {
		t.Error("最も古い Retain が残っている")
	}
	for i := 1; i < 17; i++ {
		if _, ok := un.Since(fmt.Sprintf("%%%d", i)); !ok {
			t.Errorf("%%%d を捨てた", i)
		}
	}
	un.Mark("%x", now) // Retain していない記録は数えない
	if _, ok := un.Since("%1"); !ok {
		t.Error("Retain していない記録を足しただけで捨てた")
	}
}

// 画面が working・asking と読めたら見張りをやめる。
func TestRetainedPaneStopsWhenScreenWorks(t *testing.T) {
	g, _ := retainedRig(t)
	g.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "a", "build"))
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("%s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("画面が working になったのに見張りが残っている")
	}

	g, _ = retainedRig(t)
	g.env.Tmux.SetScreen("%0", panetest.ScreenAsk("omp", "どちらにしますか？", 1, true, "A", "B"))
	if got := g.state(t); got != agent.StateAsking {
		t.Fatalf("%s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("画面が asking になったのに見張りが残っている")
	}
}

// shell に戻ったら見張りをやめる。
func TestRetainedPaneStopsWhenBackToShell(t *testing.T) {
	g, _ := retainedRig(t)
	g.env.Tmux.SetScreen("%0", panetest.ScreenShell())
	g.env.Host.SetTree(100, []string{"bash"}) // omp が終わった
	if got := g.state(t); got != agent.StateShell {
		t.Fatalf("%s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("shell に戻ったのに見張りが残っている")
	}
}

// ペインが消えたら（続けて 2 回のスナップショットに無ければ）見張りをやめる。
func TestRetainedPaneStopsWhenPaneVanishes(t *testing.T) {
	g, _ := retainedRig(t)
	other := g.env.Pane("%1", 200, 0, 50, panetest.ScreenShell(), nil)
	g.env.Tmux.SetPanes(other)
	mustRefresh(t, g.reg)
	if _, ok := g.un.Since("%0"); !ok {
		t.Fatal("1 回見えなかっただけで捨てた")
	}
	mustRefresh(t, g.reg)
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("ペインが消えたのに見張りが残っている")
	}
}

// 同じペイン ID に別の omp（pid が違う）がいたら見張りをやめる（tmux の再起動で ID が使い回されたときなど）。
func TestRetainedPaneStopsWhenAgentProcessChanges(t *testing.T) {
	g, _ := retainedRig(t)
	g.idleFor(t, time.Minute)
	g.env.Tmux.SetPanes(g.env.Pane("%0", 300, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}))
	g.probe.AddWork(301, g.env.Clock.Now()) // 新しい omp のログ（Enter より後）
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("別のプロセスのログで %s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("別のプロセスなのに見張りが残っている")
	}
}

// 次の指示が受理されたら（Clear）見張りをやめ、受理されなかったら（Mark）10 分の上限に戻る。
func TestNextDispatchResetsRetainedWatch(t *testing.T) {
	g, _ := retainedRig(t)
	g.un.Mark("%0", g.env.Clock.Now()) // 次の指示も受理を確かめられなかった
	g.idleFor(t, 11*time.Minute)
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("Mark し直したあとも 10 分の上限が外れたまま")
	}

	g, _ = retainedRig(t)
	g.un.Clear("%0") // 次の指示が受理された
	g.probe.AddWork(logPID, g.env.Clock.Now())
	g.env.Clock.Advance(time.Minute)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("受理された指示のあとにログで %s", got)
	}
}
