package webhooktests

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// pane.idle_report（Issue #93。FR-STATE-1〜3）の、公開の API だけで確かめられる動き。

func idleRig(t *testing.T) (*rig, *receiver) {
	t.Helper()
	r := newReceiver(t, "KEY-I-0000", "TOK-I-0000")
	return newRig(t, r.target(3)), r
}

// working→idle とレポート作成で POST が 1 回だけ届く。同じ回では、そのあと何度スナップショットを見ても、レポートを更新しても、重ねて送らない。
func TestPostsOnceWhenWorkingToIdleWithReport(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "done\n") // 作業中に最終報告が書かれた
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle)

	if got := len(r.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1", got)
	}
	pl := payloadOf(t, r.received()[0])
	want := webhook.Payload{
		Event: webhook.EventPaneIdleReport, EventID: pl.EventID, Session: "0", Pane: "T2", PaneID: "%3",
		Note:           webhook.Note{From: "そら", Repo: "ousiassllc/swarm-console", Items: []string{"#93"}, Next: "PR を出す"},
		ReportPath:     pl.ReportPath,
		StateChangedAt: g.at.Format(time.RFC3339),
	}
	if pl.EventID == "" || !strings.HasSuffix(pl.ReportPath, "/reports/0_T2.md") || pl.Event != want.Event || pl.Session != want.Session ||
		pl.Pane != want.Pane || pl.PaneID != want.PaneID || pl.Note.From != want.Note.From || pl.StateChangedAt != want.StateChangedAt ||
		len(pl.Note.Items) != 1 || pl.Note.Next != want.Note.Next || pl.Note.Repo != want.Note.Repo {
		t.Errorf("payload\n got=%+v\nwant=%+v", pl, want)
	}

	g.see("0", agent.StateIdle) // 同じ回
	g.report("0", "done again, longer\n")
	g.see("0", agent.StateIdle)
	g.see("0", agent.StateIdle)
	if got := len(r.received()); got != 1 {
		t.Fatalf("同じ回で重ねて送っています: %d 回", got)
	}
}

// 次の回（working に戻って、また idle）は別のイベントとして 1 回送る。event_id が違う。
func TestNextRoundPostsAgainWithNewEventID(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "one\n")
	g.see("0", agent.StateIdle)
	g.see("0", agent.StateWorking) // 新しい指示
	g.report("0", "two, and longer\n")
	g.see("0", agent.StateIdle)

	hits := r.received()
	if len(hits) != 2 {
		t.Fatalf("POST = %d 回, want 2", len(hits))
	}
	if a, b := payloadOf(t, hits[0]).EventID, payloadOf(t, hits[1]).EventID; a == b {
		t.Errorf("回が違うのに event_id が同じ: %s", a)
	}
}

// idle になったあとにレポートが書かれる順でも、書かれたときに 1 回だけ送る。書かれるまでは送らない。
func TestReportWrittenAfterIdlePostsOnce(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle)
	g.see("0", agent.StateIdle)
	if len(r.received()) != 0 {
		t.Fatal("レポートが無いのに送りました")
	}
	g.report("0", "done\n")
	g.see("0", agent.StateIdle)
	g.report("0", "done, more\n")
	g.see("0", agent.StateIdle)
	if got := len(r.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1", got)
	}
}

// 前の回のレポートが残っているだけで、今回は書かれなかったら送らない（working を見たときと同じファイルなら「書けた」ではない）。
func TestStaleReportFromEarlierRoundDoesNotTrigger(t *testing.T) {
	g, r := idleRig(t)
	g.report("0", "stale\n") // 前の回の残り
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateIdle)
	g.see("0", agent.StateIdle)
	if len(r.received()) != 0 {
		t.Fatal("今回書かれていないレポートで送りました")
	}
}

// working のあとに asking・unknown・stalled をはさんでも同じ回で、idle とレポートで pane.idle_report が 1 回だけ届く
// （asking は別のイベント pane.asking。FR-STATE-9）。shell に戻ったら回を捨てる（そのあとの idle では送らない）。
// working を見ていない（回を持たない）ペインは、asking をはさんで idle になり、レポートが書かれても pane.idle_report は送らない。
func TestRoundSurvivesAskingAndUnknown(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.see("0", agent.StateAsking)
	g.see("0", agent.StateUnknown)
	g.report("0", "done\n")
	g.see("0", agent.StateStalled)
	g.see("0", agent.StateIdle)
	if got := count(t, r, webhook.EventPaneIdleReport); got != 1 {
		t.Fatalf("pane.idle_report = %d 回, want 1", got)
	}
	if got := count(t, r, webhook.EventPaneAsking); got != 1 {
		t.Fatalf("pane.asking = %d 回, want 1", got)
	}

	g2, r2 := idleRig(t)
	g2.see("0", agent.StateWorking)
	g2.see("0", agent.StateShell)
	g2.report("0", "done\n")
	g2.see("0", agent.StateIdle)
	if len(r2.received()) != 0 {
		t.Fatal("shell に戻った回で送りました")
	}
	g2.see("0", agent.StateAsking) // working を見ていない回で質問が出て閉じても、報告の件は送らない
	g2.report("0", "done, and longer\n")
	g2.see("0", agent.StateIdle)
	if got := count(t, r2, webhook.EventPaneIdleReport); got != 0 {
		t.Fatal("working を見ていない回で pane.idle_report を送りました")
	}
}

// 署名と Bearer は本文と送り先の値で付く。署名・トークンを設定しなければ、そのヘッダーは付けない。
func TestSignatureAndBearerHeaders(t *testing.T) {
	g, r := idleRig(t)
	g.see("0", agent.StateWorking)
	g.report("0", "done\n")
	g.see("0", agent.StateIdle)
	h := r.received()[0]
	assertSigned(t, r, h)
	if h.header.Get("Content-Type") != "application/json" || h.header.Get("X-Swarm-Console-Event-Id") != payloadOf(t, h).EventID {
		t.Errorf("ヘッダー: %v", h.header)
	}

	bare := newReceiver(t, "", "")
	cfg := bare.target(3)
	cfg.Secret, cfg.Token = "", ""
	g2 := newRig(t, config.Webhook{URL: cfg.URL, MaxAttempts: 3, RetryInterval: time.Second, Timeout: 5 * time.Second})
	g2.see("0", agent.StateWorking)
	g2.report("0", "done\n")
	g2.see("0", agent.StateIdle)
	hdr := bare.received()[0].header
	if hdr.Get("X-Swarm-Console-Signature") != "" || hdr.Get("Authorization") != "" {
		t.Errorf("設定していないのに付いています: %v", hdr)
	}
}
