package webhooktests

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

func defaultTarget(r *receiver) config.Webhook { return r.target(3) }

// セッション 0 と 1 が別々の URL に、それぞれ 1 回だけ届く。署名・トークンはセッションごとの値で、
// 何度スナップショットを渡しても重ならず、[webhook] の URL にはどちらも届かない。
func TestEachSessionPostsOnceToItsOwnURL(t *testing.T) {
	def := newReceiver(t, "KEY-D-0000", "TOK-D-0000")
	a := newReceiver(t, "KEY-A-1111", "TOK-A-2222")
	b := newReceiver(t, "KEY-B-3333", "TOK-B-4444")
	cfg := defaultTarget(def)
	cfg.Sessions = map[string]config.Webhook{"0": a.target(3), "1": b.target(3)}
	g := newRig(t, cfg)

	g.idleRound("0", "1")

	ha, hb := a.received(), b.received()
	if len(ha) != 1 || len(hb) != 1 {
		t.Fatalf("セッション 0 = %d 回・セッション 1 = %d 回, want 1・1\n%s", len(ha), len(hb), g.logText())
	}
	if got := len(def.received()); got != 0 {
		t.Errorf("[webhook] の URL にも %d 回届きました", got)
	}
	pa, pb := payloadOf(t, ha[0]), payloadOf(t, hb[0])
	if pa.Session != "0" || pa.PaneID != "%3" || pb.Session != "1" || pb.PaneID != "%4" {
		t.Errorf("payload が違います: %+v / %+v", pa, pb)
	}
	if pa.EventID == pb.EventID {
		t.Errorf("別のセッションの event_id が同じです: %s", pa.EventID)
	}
	assertSigned(t, a, ha[0])
	assertSigned(t, b, hb[0])
	g.assertNoSecrets(def, a, b)
}

// セッションの表が無いセッションは [webhook] に送る。表があるセッションは自分の送り先だけに送る。
func TestSessionWithoutOwnTargetFallsBackToDefault(t *testing.T) {
	def := newReceiver(t, "KEY-D-0000", "TOK-D-0000")
	b := newReceiver(t, "KEY-B-3333", "TOK-B-4444")
	cfg := defaultTarget(def)
	cfg.Sessions = map[string]config.Webhook{"1": b.target(3)}
	g := newRig(t, cfg)

	g.idleRound("0", "1")

	hd, hb := def.received(), b.received()
	if len(hd) != 1 || len(hb) != 1 {
		t.Fatalf("[webhook] = %d 回・セッション 1 = %d 回, want 1・1", len(hd), len(hb))
	}
	if payloadOf(t, hd[0]).Session != "0" || payloadOf(t, hb[0]).Session != "1" {
		t.Error("届いたセッションが違います")
	}
	assertSigned(t, def, hd[0])
	assertSigned(t, b, hb[0])
	g.assertNoSecrets(def, b)
}

// [webhook] の url が無く、セッション 1 の表だけがあるとき、セッション 0 は何も送らず、台帳にも件を作らない。
func TestSessionWithNoTargetSendsNothing(t *testing.T) {
	b := newReceiver(t, "KEY-B-3333", "TOK-B-4444")
	cfg := config.Webhook{MaxAttempts: 3, RetryInterval: time.Minute, Timeout: 5 * time.Second, Sessions: map[string]config.Webhook{"1": b.target(3)}}
	g := newRig(t, cfg)

	g.idleRound("0", "1")

	hb := b.received()
	if len(hb) != 1 || payloadOf(t, hb[0]).Session != "1" {
		t.Fatalf("セッション 1 = %d 回, want 1\n%s", len(hb), g.logText())
	}
	if l := g.ledger(); !strings.Contains(l, `"outbox":{}`) {
		t.Errorf("outbox に件が残っています: %s", l)
	}
	if strings.Contains(g.logText(), `"session":"0"`) {
		t.Errorf("送り先の無いセッション 0 の送信のログがあります:\n%s", g.logText())
	}
}

// 全部のセッションに送り先が無ければ（url が全部空）、Notifier は作れない（呼び出し側は作らない）。
func TestNoTargetAtAllIsNotBuilt(t *testing.T) {
	_, err := webhook.New(webhook.Options{
		Config: config.Webhook{Sessions: map[string]config.Webhook{"0": {}}}, ReportDir: t.TempDir(), StatePath: filepath.Join(t.TempDir(), "s.json"),
	})
	if err == nil {
		t.Fatal("送り先が 1 つも無いのに作れました")
	}
}

// セッションの表の url が空なら、[webhook] があっても、そのセッションだけ送らない。
func TestSessionWithEmptyURLOptsOutOfDefault(t *testing.T) {
	def := newReceiver(t, "KEY-D-0000", "TOK-D-0000")
	cfg := defaultTarget(def)
	cfg.Sessions = map[string]config.Webhook{"0": {}}
	g := newRig(t, cfg)

	g.idleRound("0", "1")

	hd := def.received()
	if len(hd) != 1 || payloadOf(t, hd[0]).Session != "1" {
		t.Fatalf("[webhook] への POST = %d 回, want セッション 1 の 1 回", len(hd))
	}
}

// 再送の設定もセッションごと。セッション 1 が落ちていて max_attempts = 2 なら 2 回で諦め、セッション 0（max_attempts = 4）は影響を受けない。
func TestRetrySettingsArePerSession(t *testing.T) {
	a := newReceiver(t, "KEY-A-1111", "TOK-A-2222")
	b := newReceiver(t, "KEY-B-3333", "TOK-B-4444")
	b.setStatus(http.StatusServiceUnavailable)
	cfg := config.Webhook{Sessions: map[string]config.Webhook{"0": a.target(4), "1": b.target(2)}}
	g := newRig(t, cfg)

	g.idleRound("0", "1")

	if got := len(a.received()); got != 1 {
		t.Errorf("セッション 0 = %d 回, want 1", got)
	}
	if got := len(b.received()); got != 2 {
		t.Errorf("セッション 1 = %d 回, want 2（max_attempts）", got)
	}
	g.assertNoSecrets(a, b)
}

// 台帳から引き継いだ件のセッションに、今の設定では送り先が無いとき（設定を変えて再起動）は、送らずに諦めてログに残す。
func TestLoadedItemWithoutTargetIsDroppedWithoutPosting(t *testing.T) {
	g := bareRig(t)
	body := `{"event":"pane.idle_report","event_id":"evt_s1","session":"1","pane":"T2","pane_id":"%4","note":{"from":"","repo":"","items":[],"next":""},"report_path":"","state_changed_at":""}`
	led := `{"version":2,"panes":{},"outbox":{"evt_s1":{"event_id":"evt_s1","payload":` + body + `,"attempts":0}}}`
	if err := os.WriteFile(g.state, []byte(led), 0o600); err != nil {
		t.Fatal(err)
	}
	g.build(config.Webhook{Sessions: map[string]config.Webhook{"1": {}, "0": {URL: "http://127.0.0.1:1/x", MaxAttempts: 1, Timeout: time.Second}}})
	g.drain()

	if logs := g.logText(); !strings.Contains(logs, `"reason":"no_destination"`) {
		t.Errorf("諦めたことがログにありません:\n%s", logs)
	}
	if l := g.ledger(); strings.Contains(l, "evt_s1") {
		t.Errorf("件が台帳に残っています: %s", l)
	}
}
