package webhooktests

import (
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// Issue #115: pane.idle_report を送ったあとも omp が作業を続け、最終報告を書き直してもう一度 idle になったら、
// 新しい回として 2 件目を（新しい event_id で）1 回だけ送る。書き直していなければ、何度 working と idle を行き来しても送らない。

// 画面が working に戻るケース。1 件目のあと、working を見る前に最終報告が書き直されていても（working を最初に見たときの
// 最終報告はもう書き直されたあと）、その回の idle で「書けた」と読み、2 件目が 1 回だけ飛ぶ。
func TestRewrittenReportAfterFirstPostPostsSecondOnce(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "one\n")
	g.see("0", agent.StateIdle) // 1 件目

	g.report("0", "one, rewritten and longer\n") // idle のまま作業が続き、working と読む前に書き直した
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle) // 2 件目
	for range 3 {
		g.see("0", agent.StateIdle)
	}

	hits := r.received()
	if len(hits) != 2 {
		t.Fatalf("POST = %d 回, want 2\n%s", len(hits), g.logText())
	}
	if a, b := payloadOf(t, hits[0]).EventID, payloadOf(t, hits[1]).EventID; a == b {
		t.Errorf("回が違うのに event_id が同じ: %s", a)
	}
}

// working に戻ってから書き直す普通の順でも 2 件目は 1 回（#93 からの動き）。
func TestReportRewrittenWhileWorkingAgainPostsSecondOnce(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "one\n")
	g.see("0", agent.StateIdle)

	g.see("0", agent.StateWorking)
	g.report("0", "one, rewritten and longer\n")
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle)
	g.see("0", agent.StateIdle)

	if got := len(r.received()); got != 2 {
		t.Fatalf("POST = %d 回, want 2\n%s", got, g.logText())
	}
}

// 最終報告を書き直さずに working と idle を行き来するだけなら、2 件目は送らない（1 件目のあと・何度行き来しても）。
func TestWorkingIdleFlipsWithoutRewritePostNoSecond(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "one\n")
	g.see("0", agent.StateIdle) // 1 件目
	for range 4 {
		g.see("0", agent.StateWorking)
		g.see("0", agent.StateIdle)
		g.see("0", agent.StateIdle)
	}
	if got := len(r.received()); got != 1 {
		t.Fatalf("書き直していないのに送りました: POST = %d 回, want 1", got)
	}
}

// 1 件目のあとの idle で台帳に覚えた「送ったときの最終報告」は、serve を再起動しても残る。
// 再起動をはさんでも、書き直した最終報告で 2 件目が 1 回だけ飛び、書き直していなければ飛ばない。
func TestSecondPostSurvivesRestart(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "one\n")
	g.see("0", agent.StateIdle) // 1 件目
	g.report("0", "one, rewritten and longer\n")

	g.drain()
	g.build(r.target(3))
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle)
	if got := len(r.received()); got != 2 {
		t.Fatalf("再起動後: POST = %d 回, want 2\n%s", got, g.logText())
	}

	g.drain()
	g.build(r.target(3))
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle) // 書き直していない
	if got := len(r.received()); got != 2 {
		t.Fatalf("再起動後に書き直さず送りました: POST = %d 回, want 2", got)
	}
}

// 画面が変わらないまま作業が続くケース（#113 の起動画面のまま止まった omp）。1 件目（ログの作業の跡が 2 分途切れて idle）のあと、
// ログに作業の跡が戻れば working と読み、最終報告を書き直して作業の跡が途切れたら、2 件目が 1 回だけ飛ぶ。
func TestFrozenScreenWorkContinuesAfterFirstPostPostsSecondOnce(t *testing.T) {
	r := newReceiver(t, "KEY-F-0001", "TOK-F-0001")
	f := newFrozenScreen(t, r)

	f.see(time.Second)
	f.un.Mark("%0", f.env.Clock.Now())
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	if got := f.see(5 * time.Second); got != agent.StateWorking {
		t.Fatalf("起動画面のまま作業しているのに %s", got)
	}
	f.writeReportBody("first\n")
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	f.see(5 * time.Second)
	if got := f.see(3 * time.Minute); got != agent.StateIdle { // 作業の跡が途切れた
		t.Fatalf("作業の跡が途切れたのに %s", got)
	}
	f.see(5 * time.Second)
	f.settle()
	if n := len(r.received()); n != 1 {
		t.Fatalf("1 件目: POST = %d 回, want 1\n%s", n, f.logText())
	}

	// 作業が続く。最終報告は、ログの作業の跡が戻って working と読む前に書き直される。
	f.see(2 * time.Minute) // 記録（Unaccepted）は 10 分以内なので、まだ見ている
	f.writeReportBody("first, then rewritten and longer\n")
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	if got := f.see(5 * time.Second); got != agent.StateWorking {
		t.Fatalf("作業が続いているのに %s", got)
	}
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	f.see(30 * time.Second)
	if got := f.see(3 * time.Minute); got != agent.StateIdle {
		t.Fatalf("作業の跡が途切れたのに %s", got)
	}
	for range 5 {
		f.see(5 * time.Second)
	}
	f.drain()

	hits := r.received()
	if len(hits) != 2 {
		t.Fatalf("POST = %d 回, want 2\n%s", len(hits), f.logText())
	}
	a, b := payloadOf(t, hits[0]), payloadOf(t, hits[1])
	if b.Event != webhook.EventPaneIdleReport || a.EventID == b.EventID {
		t.Errorf("2 件目 = %+v（1 件目の event_id %s）", b, a.EventID)
	}
}

// 画面が変わらないまま作業の跡が戻っても、最終報告を書き直していなければ 2 件目は送らない。
func TestFrozenScreenWorkWithoutRewritePostsNoSecond(t *testing.T) {
	r := newReceiver(t, "KEY-F-0002", "TOK-F-0002")
	f := newFrozenScreen(t, r)

	f.see(time.Second)
	f.un.Mark("%0", f.env.Clock.Now())
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	f.see(5 * time.Second)
	f.writeReportBody("first\n")
	f.see(3 * time.Minute) // idle。1 件目
	f.see(5 * time.Second)
	f.settle()

	for range 3 { // 作業の跡が戻っては途切れる。最終報告はそのまま
		f.see(2 * time.Minute)
		f.probe.AddWork(frozenPID, f.env.Clock.Now())
		// 画面が変わらないまま 15 分たつと stalled になる（回では working と同じ扱い）。
		if got := f.see(5 * time.Second); got != agent.StateWorking && got != agent.StateStalled {
			t.Fatalf("作業が続いているのに %s", got)
		}
		f.see(3 * time.Minute)
	}
	f.drain()
	if n := len(r.received()); n != 1 {
		t.Fatalf("書き直していないのに送りました: POST = %d 回, want 1\n%s", n, f.logText())
	}
}
