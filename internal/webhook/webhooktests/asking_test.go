package webhooktests

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// pane.asking（Issue #111。FR-STATE-9〜11）。公開の API だけで確かめる。

const (
	sdkTitle    = "SDK: Decide SDK distribution acceptance criterion"
	sdkQuestion = "受け入れ条件「SDK を配布できる」の扱い"
)

// asking は session のペイン（%3・T2）が質問で止まっている状態。activity は質問の要約。
func asking(session, activity string) pane.PaneState {
	p := paneIn(session, agent.StateAsking)
	p.Title, p.Activity = sdkTitle, activity
	return p
}

func askingRig(t *testing.T) (*rig, *receiver) {
	t.Helper()
	r := newReceiver(t, "KEY-Q-0000", "TOK-Q-0000")
	return newRig(t, r.target(3)), r
}

func askingBody(t *testing.T, h hit) webhook.AskingPayload {
	t.Helper()
	var pl webhook.AskingPayload
	if err := json.Unmarshal(h.body, &pl); err != nil {
		t.Fatal(err)
	}
	return pl
}

// ペインが asking になったら pane.asking が 1 件届く。本文はペインの位置・タイトル・質問の要約・event_id・見た時刻だけ。
// 同じ回では、そのあと何度見ても、質問の要約が変わっても、unknown をはさんでも重ねて送らない。
func TestAskingPostsOncePerAskingRound(t *testing.T) {
	g, r := askingRig(t)
	g.see("0", agent.StateWorking)
	g.observe("", asking("0", sdkQuestion))
	at := g.at

	hits := r.received()
	if len(hits) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(hits))
	}
	got := askingBody(t, hits[0])
	if !strings.HasPrefix(got.EventID, "evt_") {
		t.Errorf("event_id = %q", got.EventID)
	}
	want := webhook.AskingPayload{
		Event: webhook.EventPaneAsking, EventID: got.EventID, Session: "0", Pane: "T2", PaneID: "%3",
		Title: sdkTitle, Question: sdkQuestion, StateChangedAt: at.Format(time.RFC3339),
	}
	if got != want {
		t.Errorf("payload\n got=%+v\nwant=%+v", got, want)
	}
	var keys map[string]any
	if err := json.Unmarshal(hits[0].body, &keys); err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"note", "report_path"} { // 画面・メモ・報告の中身は送らない
		if _, ok := keys[banned]; ok {
			t.Errorf("pane.asking の本文に %s があります: %v", banned, keys)
		}
	}
	if len(keys) != 8 {
		t.Errorf("本文の項目 = %d 個, want 8: %v", len(keys), keys)
	}
	assertSigned(t, r, hits[0])

	g.observe("", asking("0", sdkQuestion)) // 同じ質問のまま
	g.see("0", agent.StateUnknown)          // 一時的な判定不能をはさんでも同じ回
	g.observe("", asking("0", "次の質問"))      // 質問が変わっても（質問が複数のフォーム）同じ回
	g.observe("", asking("0", "次の質問"))
	if got := len(r.received()); got != 1 {
		t.Fatalf("同じ asking の回で重ねて送っています: %d 回", got)
	}
}

// asking でない状態（working・idle・stalled・shell）を見たら回が終わり、次の asking は別のイベントとして 1 件送る（event_id が違う）。
func TestNextAskingRoundPostsAgain(t *testing.T) {
	for _, between := range []agent.State{agent.StateWorking, agent.StateIdle, agent.StateStalled, agent.StateShell} {
		t.Run(string(between), func(t *testing.T) {
			g, r := askingRig(t)
			g.observe("", asking("0", "1 つ目"))
			g.see("0", between)
			g.observe("", asking("0", "2 つ目"))

			hits := r.received()
			if len(hits) != 2 {
				t.Fatalf("pane.asking = %d 回, want 2", len(hits))
			}
			if a, b := askingBody(t, hits[0]), askingBody(t, hits[1]); a.EventID == b.EventID || a.Question != "1 つ目" || b.Question != "2 つ目" {
				t.Errorf("1 つ目 %+v, 2 つ目 %+v", a, b)
			}
		})
	}
}

// 質問の回は pane.idle_report の回とは別。event_id は重ならず、報告の件はそのまま 1 件届く。
func TestAskingAndIdleReportAreIndependent(t *testing.T) {
	g, r := askingRig(t)
	g.see("0", agent.StateWorking)
	g.observe("", asking("0", "質問"))
	g.report("0", "done\n")
	g.see("0", agent.StateIdle)

	if count(t, r, webhook.EventPaneAsking) != 1 || count(t, r, webhook.EventPaneIdleReport) != 1 {
		t.Fatalf("pane.asking = %d, pane.idle_report = %d, want 1・1", count(t, r, webhook.EventPaneAsking), count(t, r, webhook.EventPaneIdleReport))
	}
	if a, b := payloadOf(t, r.received()[0]).EventID, payloadOf(t, r.received()[1]).EventID; a == b {
		t.Error("event_id が重なっています")
	}
}

// 再起動しても、同じ質問のまま（台帳に asking の回が残っている間）は送り直さない。質問が終わって次の質問になれば送る。
func TestRestartDoesNotResendSameAskingRound(t *testing.T) {
	r := newReceiver(t, "KEY-Q-0000", "TOK-Q-0000")
	cfg := r.target(3)
	g := newRig(t, cfg)
	restart := func() { g.cancel(); g.wait(); g.build(cfg) }

	g.observe("", asking("0", "質問"))
	restart()
	g.observe("", asking("0", "質問"))
	g.observe("", asking("0", "質問"))
	if got := len(r.received()); got != 1 {
		t.Fatalf("再起動のあとに重ねて送りました: %d 回", got)
	}

	g.see("0", agent.StateWorking)
	restart()
	g.observe("", asking("0", "次の質問"))
	if got := len(r.received()); got != 2 {
		t.Fatalf("再起動をまたいだ次の質問が届いていません: %d 回", got)
	}
}

// tmux が再起動したら（ペイン ID が使い回される）asking の回も捨て、次に asking を見たら送る。
// 消えたペインは、続けて 2 回スナップショットに無ければ捨てる（1 回の欠けでは捨てない）。
func TestTmuxRestartAndVanishedPaneEndAskingRound(t *testing.T) {
	g, r := askingRig(t)
	g.observe("100", asking("0", "質問"))
	g.observe("200", asking("0", "質問")) // tmux が再起動した
	if got := len(r.received()); got != 2 {
		t.Fatalf("tmux の再起動のあとの asking が届いていません: %d 回", got)
	}

	other := paneIn("0", agent.StateWorking)
	other.ID, other.Pos, other.Local, other.GlobalPos = "%9", "T1", "T1", "0:T1"
	g.observe("200", other) // %3 が 1 回無い
	if !strings.Contains(g.ledger(), `"asked":{"%3"`) {
		t.Fatalf("1 回無かっただけで asking の回を捨てました: %s", g.ledger())
	}
	g.observe("200", asking("0", "質問"), other) // 戻ってきた。同じ回
	g.observe("200", other)
	g.observe("200", other) // 続けて 2 回無い
	if strings.Contains(g.ledger(), `"%3"`) {
		t.Fatalf("消えたペインの asking の回が残っています: %s", g.ledger())
	}
	if got := len(r.received()); got != 2 {
		t.Fatalf("同じ回を送り直しました: %d 回", got)
	}
}

// セッションごとの送り先（FR-STATE-8）に従う。表のあるセッションはその表の送り先、url が空の表のセッションと、
// 送り先の無いセッションは送らず、台帳にも件を作らない。
func TestAskingFollowsSessionDestination(t *testing.T) {
	def := newReceiver(t, "KEY-D-0000", "TOK-D-0000")
	own := newReceiver(t, "KEY-A-1111", "TOK-A-2222")
	cfg := def.target(3)
	cfg.Sessions = map[string]config.Webhook{"own": own.target(3), "quiet": {}}
	g := newRig(t, cfg)

	mk := func(session, id string) pane.PaneState {
		p := asking(session, "質問 "+session)
		p.ID, p.GlobalPos = id, session+":T2"
		return p
	}
	g.observe("", mk("0", "%3"), mk("own", "%4"), mk("quiet", "%5"))
	g.observe("", mk("0", "%3"), mk("own", "%4"), mk("quiet", "%5"))

	hd, ho := def.received(), own.received()
	if len(hd) != 1 || len(ho) != 1 || askingBody(t, hd[0]).Session != "0" || askingBody(t, ho[0]).Session != "own" {
		t.Fatalf("[webhook] = %d 回・own = %d 回, want 1・1", len(hd), len(ho))
	}
	assertSigned(t, def, hd[0])
	assertSigned(t, own, ho[0])
	if strings.Contains(g.logText(), `"session":"quiet"`) {
		t.Errorf("送らないセッションの送信のログがあります:\n%s", g.logText())
	}
	g.assertNoSecrets(def, own)
}

// 秘密の値（URL・署名の鍵・トークン）は、成功・503・接続不能のどの経路でも、pane.asking の本文・ログ・台帳に出ない（NFR-SEC-18）。
func TestAskingNeverLeaksSecrets(t *testing.T) {
	for name, setup := range map[string]func(r *receiver){
		"成功":   func(*receiver) {},
		"503":  func(r *receiver) { r.setStatus(http.StatusServiceUnavailable) },
		"接続不能": func(r *receiver) { r.srv.Close() },
	} {
		t.Run(name, func(t *testing.T) {
			g, r := askingRig(t)
			setup(r)
			g.observe("", asking("0", "質問"))
			out := g.logText() + "\n" + g.ledger()
			for _, h := range r.received() {
				out += "\n" + string(h.body)
			}
			for _, s := range r.secrets() {
				if strings.Contains(out, s) {
					t.Errorf("%q が出ています:\n%s", s, out)
				}
			}
			if !strings.Contains(g.ledger(), `"asked"`) {
				t.Errorf("台帳に asking の回がありません: %s", g.ledger())
			}
		})
	}
}

// 台帳に asking の回の記録がない（`asked` が無い）古い台帳でも読め、そのまま使える。
func TestLedgerWithoutAskedStillLoads(t *testing.T) {
	r := newReceiver(t, "KEY-Q-0000", "TOK-Q-0000")
	g := bareRig(t)
	if err := os.WriteFile(g.state, []byte(`{"version":2,"panes":{},"outbox":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g.build(r.target(3))
	g.observe("", asking("0", "質問"))
	if got := len(r.received()); got != 1 || strings.Contains(g.logText(), "台帳") {
		t.Fatalf("POST = %d 回\n%s", got, g.logText())
	}
}
