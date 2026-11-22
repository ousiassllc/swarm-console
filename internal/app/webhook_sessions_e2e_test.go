package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

// Issue #95：セッションごとの送り先。webhook_e2e_test.go の rig（実 tmux のセッションごとの擬似エージェント）を使う。

// 2 つのセッション（実 tmux）のペインを working にしてから、それぞれの最終報告を書いて idle にする。
// セッションが複数あると、最終報告のファイル名は <セッション>_<位置>.md。
func (g *rig) idleRoundAll(sessions ...string) {
	g.t.Helper()
	for _, s := range sessions {
		g.untilPane(g.panes[s], agent.StateWorking)
	}
	for _, s := range sessions {
		if err := os.WriteFile(filepath.Join(g.reports, s+"_T1.md"), []byte("done "+s+"\n"), 0o600); err != nil {
			g.t.Fatal(err)
		}
		g.tm.Run("send-keys", "-t", g.panes[s], "Escape")
	}
	for _, s := range sessions {
		g.untilPane(g.panes[s], agent.StateIdle)
	}
}

func (g *rig) untilPane(id string, want agent.State) {
	g.t.Helper()
	tmuxtest.Eventually(g.t, id+" が "+string(want)+" になる", func() bool {
		snap, err := g.app.Registry.Refresh(context.Background())
		if err != nil {
			g.t.Fatal(err)
		}
		p, ok := snap.Find(id)
		return ok && p.State == want
	})
}

const (
	e2eToken1  = "E2E-TOKEN1-9b3e"  //nolint:gosec // テスト用のダミー値
	e2eSecret1 = "E2E-SIGKEY1-51d0" //nolint:gosec // テスト用のダミー値
	e2ePath1   = "E2E-URLPATH1-7e2a"
)

// セッションごとの送り先を書いた設定。セッション 0 は a、1 は b（鍵・トークンも別）。
func sessionWebhookCfg(a, b string) string {
	return fmt.Sprintf(`[webhook.sessions."0"]
url = %q
secret = %q
token = %q
max_attempts = 2
retry_interval = "1s"

[webhook.sessions."1"]
url = %q
secret = %q
token = %q
max_attempts = 2
retry_interval = "1s"
`, a, e2eSecret, e2eToken, b, e2eSecret1, e2eToken1)
}

func assertNoSessionSecrets(t *testing.T, name, text string) {
	t.Helper()
	assertNoSecrets(t, name, text)
	for _, secret := range []string{e2eToken1, e2eSecret1, e2ePath1} {
		if strings.Contains(text, secret) {
			t.Errorf("%s に秘密の値 %q が出ています:\n%s", name, secret, text)
		}
	}
}

// Issue #95: 実 tmux の 2 セッション（0・1）が、別々の URL に、それぞれ 1 回だけ届く（二重に届かない）。
// どちらの受け手にも、もう一方のセッションの件は届かない。ログ・台帳に、セッションごとの秘密の値は出ない。
func TestAppWebhookSessionsPostOnceToTheirOwnURLs(t *testing.T) {
	r0 := newReceiverToken(t, http.StatusNoContent, e2eToken)
	r1 := newReceiverToken(t, http.StatusNoContent, e2eToken1)
	g := startRigSessions(t, sessionWebhookCfg(r0.srv.URL+"/"+e2ePath, r1.srv.URL+"/"+e2ePath1), "0", "1")

	g.idleRoundAll("0", "1")
	tmuxtest.Eventually(t, "2 つのセッションの webhook が届く", func() bool { return len(r0.payloads()) >= 1 && len(r1.payloads()) >= 1 })
	g.settle()
	g.settle()

	h0, h1 := r0.payloads(), r1.payloads()
	if len(h0) != 1 || len(h1) != 1 {
		t.Fatalf("セッション 0 = %d 回・セッション 1 = %d 回, want 1・1\n%s", len(h0), len(h1), g.logs.String())
	}
	if h0[0].Session != "0" || h0[0].PaneID != g.panes["0"] || h1[0].Session != "1" || h1[0].PaneID != g.panes["1"] {
		t.Errorf("payload が違います: %+v / %+v", h0[0], h1[0])
	}
	if h0[0].EventID == h1[0].EventID {
		t.Errorf("event_id が同じです: %s", h0[0].EventID)
	}
	if want := filepath.Join(g.reports, "1_T1.md"); h1[0].ReportPath != want {
		t.Errorf("セッション 1 の report_path = %s, want %s", h1[0].ReportPath, want)
	}
	tmuxtest.Eventually(t, "台帳の送信が済む", func() bool {
		l := g.ledger()
		return strings.Count(l, `"phase":"done"`) == 2 && strings.Contains(l, `"outbox":{}`)
	})
	g.stop()
	assertNoSessionSecrets(t, "ログ", g.logs.String())
	assertNoSessionSecrets(t, "台帳", g.ledger())
}

// セッションの表が無いセッション（1）は [webhook] に送り、表があるセッション（0）は自分の送り先だけに送る。それぞれ 1 回。
func TestAppWebhookSessionWithoutTableUsesDefaultWebhook(t *testing.T) {
	def := newReceiverToken(t, http.StatusNoContent, e2eToken)
	own := newReceiverToken(t, http.StatusNoContent, e2eToken1)
	cfg := webhookCfg(def.srv.URL+"/"+e2ePath) + fmt.Sprintf("\n[webhook.sessions.\"0\"]\nurl = %q\nsecret = %q\ntoken = %q\n", own.srv.URL+"/"+e2ePath1, e2eSecret1, e2eToken1)
	g := startRigSessions(t, cfg, "0", "1")

	g.idleRoundAll("0", "1")
	tmuxtest.Eventually(t, "2 つのセッションの webhook が届く", func() bool { return len(def.payloads()) >= 1 && len(own.payloads()) >= 1 })
	g.settle()

	hd, ho := def.payloads(), own.payloads()
	if len(hd) != 1 || len(ho) != 1 || hd[0].Session != "1" || ho[0].Session != "0" {
		t.Fatalf("[webhook] = %+v・セッション 0 の表 = %+v\n%s", hd, ho, g.logs.String())
	}
	g.stop()
	assertNoSessionSecrets(t, "ログ", g.logs.String())
	assertNoSessionSecrets(t, "台帳", g.ledger())
}

// [webhook] の url が無く、セッション 1 の表だけがあるとき、セッション 0 は何も送らない（セッション 1 は 1 回）。
func TestAppWebhookSessionWithNoTargetSendsNothing(t *testing.T) {
	r1 := newReceiverToken(t, http.StatusNoContent, e2eToken1)
	cfg := fmt.Sprintf("[webhook.sessions.\"1\"]\nurl = %q\nsecret = %q\ntoken = %q\n", r1.srv.URL+"/"+e2ePath1, e2eSecret1, e2eToken1)
	g := startRigSessions(t, cfg, "0", "1")

	g.idleRoundAll("0", "1")
	tmuxtest.Eventually(t, "セッション 1 の webhook が届く", func() bool { return len(r1.payloads()) >= 1 })
	g.settle()
	g.stop()

	got := r1.payloads()
	if len(got) != 1 || got[0].Session != "1" {
		t.Fatalf("セッション 1 = %+v", got)
	}
	// 送り先の無いセッション 0 は件を作らない（送る約束の outbox が空のまま）。回は done になり、二度と作られない。
	if l := g.ledger(); !strings.Contains(l, `"outbox":{}`) {
		t.Errorf("outbox に件が残っています: %s", l)
	}
	if strings.Contains(g.logs.String(), `"session":"0"`) {
		t.Errorf("送り先の無いセッション 0 の送信のログがあります:\n%s", g.logs.String())
	}
}

// Issue #95: [webhook] だけの書き方（#93 の形）は、sessions の表が無くても、複数セッションでもそのまま動く。
// 両方のセッションの件が [webhook] の 1 つの URL に、1 回ずつ届く。
func TestAppWebhookOnlyConfigWithTwoSessionsStillWorks(t *testing.T) {
	rcv := newReceiver(t, http.StatusNoContent)
	g := startRigSessions(t, webhookCfg(rcv.srv.URL+"/"+e2ePath), "0", "1")

	g.idleRoundAll("0", "1")
	tmuxtest.Eventually(t, "2 件が届く", func() bool { return len(rcv.payloads()) >= 2 })
	g.settle()
	g.stop()

	got := rcv.payloads()
	if len(got) != 2 || got[0].Session == got[1].Session {
		t.Fatalf("webhook = %+v, want セッション 0・1 から 1 回ずつ", got)
	}
	assertNoSecrets(t, "ログ", g.logs.String())
	assertNoSecrets(t, "台帳", g.ledger())
}
