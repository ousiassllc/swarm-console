package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Issue #93: working→idle とレポート作成で POST が 1 回だけ届く。同じ回では、そのあと何度スナップショットを見ても、
// レポートを更新しても、重ねて送らない。
func TestPostsOnceWhenWorkingToIdleWithReport(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.report(1000) // 作業中に最終報告が書かれた
	h.observe(agent.StateWorking)
	h.observe(agent.StateIdle)

	if got := len(h.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1", got)
	}
	pl := h.payload(0)
	want := Payload{
		Event: EventPaneIdleReport, EventID: eventID("0", "%3", h.at), Session: "0", Pane: "T2", PaneID: "%3",
		Note:           Note{From: "そら", Repo: "ousiassllc/swarm-console", Items: []string{"#93"}, Next: "PR を出す"},
		ReportPath:     reportPath,
		StateChangedAt: h.at.Format(time.RFC3339),
	}
	if !reflect.DeepEqual(pl, want) {
		t.Errorf("payload\n got=%+v\nwant=%+v", pl, want)
	}
	if _, err := time.Parse(time.RFC3339, pl.StateChangedAt); err != nil {
		t.Errorf("state_changed_at が RFC3339 でない: %v", err)
	}

	h.observe(agent.StateIdle) // 同じ回
	h.report(2000)             // レポートを更新しても同じ回
	h.observe(agent.StateIdle)
	h.observe(agent.StateIdle)
	if got := len(h.received()); got != 1 {
		t.Fatalf("同じ回で重ねて送っています: %d 回", got)
	}
}

// 次の回（working に戻って、また idle）は別のイベントとして 1 回送る。event_id が違う。
func TestNextRoundPostsAgainWithNewEventID(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)
	h.observe(agent.StateWorking) // 新しい指示
	h.report(2000)
	h.observe(agent.StateIdle)

	if len(h.received()) != 2 {
		t.Fatalf("POST = %d 回, want 2", len(h.received()))
	}
	if a, b := h.payload(0).EventID, h.payload(1).EventID; a == b {
		t.Errorf("回が違うのに event_id が同じ: %s", a)
	}
}

// idle になったあとにレポートが書かれる順でも、書かれたときに 1 回だけ送る。書かれるまでは送らない。
func TestReportWrittenAfterIdlePostsOnce(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.observe(agent.StateIdle)
	h.observe(agent.StateIdle)
	if len(h.received()) != 0 {
		t.Fatal("レポートが無いのに送りました")
	}
	h.report(1000)
	h.observe(agent.StateIdle)
	h.report(1500)
	h.observe(agent.StateIdle)
	if got := len(h.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1", got)
	}
}

// 前の回のレポートが残っているだけで、今回は書かれなかったら送らない（working を見たときと同じファイルなら「書けた」ではない）。
func TestStaleReportFromEarlierRoundDoesNotTrigger(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.report(1000) // 前の回の残り
	h.observe(agent.StateWorking)
	h.observe(agent.StateIdle)
	h.observe(agent.StateIdle)
	if len(h.received()) != 0 {
		t.Fatal("今回書かれていないレポートで送りました")
	}
}

// 質問（asking）・判定不能（unknown）をはさんでも同じ回。asking → idle は回の終わり。
// shell に戻ったら回を捨てる（そのあとの idle では送らない）。stalled → idle も working → idle として扱う。
func TestRoundSurvivesAskingAndUnknown(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.observe(agent.StateAsking)
	h.observe(agent.StateUnknown)
	h.report(1000)
	h.observe(agent.StateStalled)
	h.observe(agent.StateIdle)
	if len(h.received()) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(h.received()))
	}

	h2 := newHarness(t)
	h2.start()
	h2.observe(agent.StateWorking)
	h2.observe(agent.StateShell)
	h2.report(1000)
	h2.observe(agent.StateIdle)
	if len(h2.received()) != 0 {
		t.Fatal("shell に戻った回で送りました")
	}
	// idle だけ（working を見ていない）で質問が出て閉じても、送らない。
	h2.observe(agent.StateAsking)
	h2.report(2000)
	h2.observe(agent.StateIdle)
	if len(h2.received()) != 0 {
		t.Fatal("working を見ていない回で送りました")
	}
}

// 再起動しても同じ回は二重に送らない。台帳を読んだ新しい Notifier が、同じ idle のペインを見ても送らない。
func TestRestartDoesNotResendSameRound(t *testing.T) {
	h := newHarness(t)
	cancel := h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(h.received()))
	}
	cancel()

	h.start() // 再起動（同じ台帳）
	h.observe(agent.StateIdle)
	h.report(3000)
	h.observe(agent.StateIdle)
	if got := len(h.received()); got != 1 {
		t.Fatalf("再起動のあとに同じ回を送り直しました: %d 回", got)
	}
}

// idle になったがレポートがまだのうちに再起動しても、待ちを引き継ぎ、書かれたら同じ event_id で 1 回送る。
func TestRestartWhileWaitingForReportContinues(t *testing.T) {
	h := newHarness(t)
	cancel := h.start()
	h.observe(agent.StateWorking)
	h.observe(agent.StateIdle)
	changedAt := h.at
	cancel()

	h.start()
	h.observe(agent.StateIdle)
	if len(h.received()) != 0 {
		t.Fatal("レポートが無いのに送りました")
	}
	h.report(1000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(h.received()))
	}
	if got, want := h.payload(0).EventID, eventID("0", "%3", changedAt); got != want {
		t.Errorf("event_id = %s, want %s（再起動の前の回と同じ）", got, want)
	}
}

// ペインが消えたら回を捨てる（tmux のペイン ID は使い回される）。
func TestVanishedPaneDropsRound(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	other := h.pane(agent.StateIdle)
	other.ID = "%9"
	h.n.Observe(&pane.Snapshot{At: h.at, Panes: []pane.PaneState{other}})
	if _, ok := h.n.entries["%3"]; ok {
		t.Fatal("消えたペインの回が残っています")
	}
}

func TestSignatureAndBearerHeaders(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)

	hit := h.received()[0]
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write(hit.body)
	if got, want := hit.header.Get(headerSignature), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("署名 = %q, want %q", got, want)
	}
	if got := hit.header.Get("Authorization"); got != "Bearer "+secretToken {
		t.Errorf("Authorization が違う")
	}
	if hit.header.Get("Content-Type") != "application/json" || hit.header.Get(headerEventID) != h.payload(0).EventID {
		t.Errorf("ヘッダー: %v", hit.header)
	}
}

// 署名・トークンを設定しなければ、そのヘッダーは付けない。
func TestNoSignatureOrAuthWhenNotConfigured(t *testing.T) {
	h := newHarness(t)
	h.cfg.Secret, h.cfg.Token = "", ""
	h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)
	hdr := h.received()[0].header
	if hdr.Get(headerSignature) != "" || hdr.Get("Authorization") != "" {
		t.Errorf("設定していないのに付いています: %v", hdr)
	}
}
