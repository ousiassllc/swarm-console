package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/testutil"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// 実際の App（実 tmux・実 HTTP）で、ペインが asking になると pane.asking が 1 回だけ届き、そのあと何度スナップショットを作っても重ねて届かない
// （Issue #111）。台帳に asking の回が残り、ログ・台帳に秘密が出ない。
func TestAppWebhookPostsAskingOnce(t *testing.T) {
	rcv := newReceiver(t, http.StatusNoContent)
	g := startRigAs(t, webhookCfg(rcv.srv.URL+"/"+e2ePath), "ask")

	g.until(agent.StateAsking)
	testutil.Eventually(t, "webhook が届く", func() bool { return len(rcv.payloads()) >= 1 })
	g.settle()

	hits := rcv.payloads()
	if len(hits) != 1 {
		t.Fatalf("webhook = %d 回, want 1\n%s", len(hits), g.logs.String())
	}
	if pl := hits[0]; pl.Event != webhook.EventPaneAsking || pl.Session != "0" || pl.Pane != "T1" || pl.PaneID != g.pane || pl.EventID == "" || pl.StateChangedAt == "" {
		t.Errorf("payload = %+v", pl)
	}
	testutil.Eventually(t, "台帳の送信が済む", func() bool {
		l := g.ledger()
		return strings.Contains(l, `"asked":{"`+g.pane+`":"`+hits[0].EventID+`"}`) && strings.Contains(l, `"outbox":{}`)
	})
	g.stop()
	assertNoSecrets(t, "ログ", g.logs.String())
	assertNoSecrets(t, "台帳", g.ledger())
}

// [webhook] を書かなければ、ペインが asking になっても何も送らず、台帳も作らない。
func TestAppWithoutWebhookSendsNothingWhenAsking(t *testing.T) {
	rcv := newReceiver(t, http.StatusNoContent)
	g := startRigAs(t, "", "ask")
	g.until(agent.StateAsking)
	g.settle()
	g.stop()

	if g.app.webhook != nil || len(rcv.payloads()) != 0 || g.ledger() != "" || strings.Contains(g.logs.String(), "webhook") {
		t.Errorf("webhook = %v, 届いた = %d, 台帳 = %q", g.app.webhook, len(rcv.payloads()), g.ledger())
	}
}
