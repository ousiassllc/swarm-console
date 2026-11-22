package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config/configtest"
	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
	"github.com/ousiassllc/swarm-console/internal/webhook"
	"github.com/ousiassllc/swarm-console/test/fakeagent/fakeagenttest"
)

const (
	e2eToken  = "E2E-TOKEN-6c1f"  //nolint:gosec // テスト用のダミー値
	e2eSecret = "E2E-SIGKEY-82aa" //nolint:gosec // テスト用のダミー値
	e2ePath   = "E2E-URLPATH-d41c"
)

// この束のテストは、実 tmux の専用ソケットに擬似エージェント（omp working）を立て、実際の App（serve と同じ配線）を動かす。
// 送信先は httptest の実 HTTP サーバー。

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// receiver は webhook の受け手。署名・トークンのヘッダーが付いた POST の本文だけを溜める。
type receiver struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []webhook.Payload
}

func newReceiver(t *testing.T, code int) *receiver { return newReceiverToken(t, code, e2eToken) }

// newReceiverToken は、Authorization: Bearer <token> と署名のヘッダーが付いた POST だけを溜める受け手。
func newReceiverToken(t *testing.T, code int, token string) *receiver {
	t.Helper()
	r := &receiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var pl webhook.Payload
		if json.Unmarshal(body, &pl) == nil && req.Header.Get("Authorization") == "Bearer "+token && req.Header.Get("X-Swarm-Console-Signature") != "" {
			r.mu.Lock()
			r.hits = append(r.hits, pl)
			r.mu.Unlock()
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) payloads() []webhook.Payload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]webhook.Payload(nil), r.hits...)
}

// rig は一時 HOME・実 tmux のペイン 1 つ・動かした App。
type rig struct {
	t       *testing.T
	app     *App
	tm      *tmuxtest.Server
	pane    string            // 最初のセッションのペイン
	panes   map[string]string // セッション名 → ペイン ID
	home    string
	reports string // report_dir（/tmp/swarm-console の下）
	logs    *syncBuf
	cancel  context.CancelFunc
	done    chan error
}

func startRig(t *testing.T, webhookCfg string) *rig { return startRigSessions(t, webhookCfg, "0") }

// startRigAs は擬似エージェントの scenario（working・ask など）でペイン 1 つを立てる。
func startRigAs(t *testing.T, webhookCfg, scenario string) *rig {
	return startRigScenario(t, webhookCfg, scenario, "0")
}

// startRigSessions は sessions の名前ごとに tmux のセッションと擬似エージェントのペインを立てて、App を動かす。
// セッションが 2 つ以上のときは、設定にも sessions を書く（最終報告のファイル名は <セッション>_<位置>.md になる）。
func startRigSessions(t *testing.T, webhookCfg string, sessions ...string) *rig {
	return startRigScenario(t, webhookCfg, "working", sessions...)
}

func startRigScenario(t *testing.T, webhookCfg, scenario string, sessions ...string) *rig {
	t.Helper()
	bin := fakeagenttest.Install(t, t.TempDir()) // HOME を替える前に go build する
	tm := tmuxtest.New(t)
	home := t.TempDir()
	reports := configtest.ReportDir(t)
	t.Setenv("HOME", home)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	cfgPath := filepath.Join(home, "config.toml")
	sessionsCfg := ""
	if len(sessions) > 1 {
		quoted := make([]string, len(sessions))
		for i, s := range sessions {
			quoted[i] = strconv.Quote(s)
		}
		sessionsCfg = "sessions = [" + strings.Join(quoted, ", ") + "]\n"
	}
	body := fmt.Sprintf("listen_addr = %q\ntmux_socket = %q\nreport_dir = %q\nupdate_check = false\nusage_check = false\n%s%s",
		addr, tm.Socket, reports, sessionsCfg, webhookCfg)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	panes := map[string]string{}
	for _, name := range sessions {
		id := tm.NewSession(name, 120, 40, fmt.Sprintf("PATH=%s:$PATH exec omp %s", bin, scenario))
		tm.WaitForText(id, "READY")
		panes[name] = id
	}
	pane := panes[sessions[0]]

	logs := &syncBuf{}
	a, err := New(cfgPath, "test", slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &rig{t: t, app: a, tm: tm, pane: pane, panes: panes, home: home, reports: reports, logs: logs, cancel: cancel, done: make(chan error, 1)}
	go func() { g.done <- a.Run(ctx) }()
	t.Cleanup(g.stop)
	return g
}

func (g *rig) stop() {
	g.cancel()
	<-g.done
	g.done <- nil // 2 回目の stop（Cleanup）でも戻れるように
}

// state はスナップショットを取り直して、ペインの状態を返す（取り直しの間に Observer の判定は終わっている）。
func (g *rig) state() agent.State {
	g.t.Helper()
	snap, err := g.app.Registry.Refresh(context.Background())
	if err != nil {
		g.t.Fatal(err)
	}
	p, ok := snap.Find(g.pane)
	if !ok {
		g.t.Fatalf("ペイン %s がありません", g.pane)
	}
	return p.State
}

func (g *rig) until(want agent.State) {
	g.t.Helper()
	tmuxtest.Eventually(g.t, "ペインが "+string(want)+" になる", func() bool { return g.state() == want })
}

// idleRound は serve が working と見たあと、最終報告を書いて Esc で idle にする。
func (g *rig) idleRound() {
	g.t.Helper()
	g.until(agent.StateWorking)
	if err := os.WriteFile(filepath.Join(g.reports, "T1.md"), []byte("done\n"), 0o600); err != nil {
		g.t.Fatal(err)
	}
	g.tm.Run("send-keys", "-t", g.pane, "Escape")
	g.until(agent.StateIdle)
}

// settle はスナップショットをあと 5 回作る。Observer の判定は取得の完了前に済むので、固定時間の待ちは要らない。
func (g *rig) settle() {
	g.t.Helper()
	for range 5 {
		g.state()
	}
}

func (g *rig) ledger() string {
	b, err := os.ReadFile(filepath.Join(g.home, ".local", "share", "swarm-console", "webhook-state.json"))
	if err != nil {
		return ""
	}
	return string(b)
}

func webhookCfg(url string) string {
	return fmt.Sprintf("[webhook]\nurl = %q\nsecret = %q\ntoken = %q\nmax_attempts = 2\nretry_interval = \"1s\"\n", url, e2eSecret, e2eToken)
}

func assertNoSecrets(t *testing.T, name, text string) {
	t.Helper()
	for _, secret := range []string{e2eToken, e2eSecret, e2ePath} {
		if strings.Contains(text, secret) {
			t.Errorf("%s に秘密の値 %q が出ています:\n%s", name, secret, text)
		}
	}
}

// 実際の App（実 tmux・実 HTTP）で、working → idle とレポートの作成で webhook が 1 回届き、そのあと何度スナップショットを作っても
// 重ねて届かない（Issue #93）。payload の項目、台帳（再起動しても二重に送らないための記録）、ログ・台帳に秘密が出ないことも確かめる。
func TestAppWebhookPostsOnceAndKeepsSecretsOutOfLogs(t *testing.T) {
	rcv := newReceiver(t, http.StatusNoContent)
	g := startRig(t, webhookCfg(rcv.srv.URL+"/"+e2ePath))

	g.idleRound()
	tmuxtest.Eventually(t, "webhook が届く", func() bool { return len(rcv.payloads()) >= 1 })
	g.settle()

	hits := rcv.payloads()
	if len(hits) != 1 {
		t.Fatalf("webhook = %d 回, want 1\n%s", len(hits), g.logs.String())
	}
	pl := hits[0]
	if pl.Event != webhook.EventPaneIdleReport || pl.Session != "0" || pl.Pane != "T1" || pl.PaneID != g.pane || pl.EventID == "" ||
		pl.ReportPath != filepath.Join(g.reports, "T1.md") || pl.StateChangedAt == "" {
		t.Errorf("payload = %+v", pl)
	}
	// 送り終えた項目は outbox から消え、回は done になる（再起動しても二重に送らない）。
	tmuxtest.Eventually(t, "台帳の送信が済む", func() bool {
		l := g.ledger()
		return strings.Contains(l, `"phase":"done"`) && strings.Contains(l, `"outbox":{}`)
	})
	g.stop()
	assertNoSecrets(t, "ログ", g.logs.String())
	assertNoSecrets(t, "台帳", g.ledger())
}

// 受け手が落ちていても、App は止まらず、再送の上限までで諦めて、ログに残す。ログに秘密の値は出ない。
func TestAppWebhookFailureIsLoggedWithoutSecrets(t *testing.T) {
	down := newReceiver(t, http.StatusServiceUnavailable)
	g := startRig(t, webhookCfg(down.srv.URL+"/"+e2ePath))

	g.idleRound()
	tmuxtest.Eventually(t, "送れなかったことがログに出る", func() bool { return strings.Contains(g.logs.String(), `"msg":"webhook を送れませんでした"`) })
	g.stop()
	logs := g.logs.String()
	assertNoSecrets(t, "ログ", logs)
	if !strings.Contains(logs, `"attempts":2`) {
		t.Errorf("回数がログにありません:\n%s", logs)
	}
	// 諦めた項目は outbox から消え、回は done のまま残る（二度と送らない）。
	tmuxtest.Eventually(t, "台帳の送信が済む", func() bool {
		l := g.ledger()
		return strings.Contains(l, `"phase":"done"`) && strings.Contains(l, `"outbox":{}`)
	})
}

// [webhook] を書かなければ（既定）、何も送らず、台帳も作らず、webhook のログも出さない。今までどおり動く。
func TestAppWithoutWebhookSendsNothing(t *testing.T) {
	rcv := newReceiver(t, http.StatusNoContent) // 立てるだけ。設定には書かない
	g := startRig(t, "")
	if g.app.webhook != nil {
		t.Fatal("webhook を書いていないのに通知ができています")
	}

	g.idleRound()
	g.settle()
	g.stop()

	if n := len(rcv.payloads()); n != 0 {
		t.Errorf("webhook を設定していないのに受け手に %d 回届きました", n)
	}
	if g.ledger() != "" {
		t.Errorf("webhook を設定していないのに台帳ができています: %s", g.ledger())
	}
	if strings.Contains(g.logs.String(), "webhook") {
		t.Errorf("webhook のログが出ています:\n%s", g.logs.String())
	}
}
