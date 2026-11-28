package webhooktests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/testutil"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// Issue #95: セッションごとの送り先。受け手はテスト内の httptest サーバーだけ（外の URL には送らない）。
// 公開の API（New・Observe・Start）だけで動かし、最終報告は一時ディレクトリの本物のファイルで作る。

type hit struct {
	header http.Header
	body   []byte
}

// receiver は 1 つの受け手。secret・token はこの受け手に付くはずの署名の鍵・トークン（URL の経路にも入れる）。
type receiver struct {
	srv    *httptest.Server
	secret string
	token  string
	mu     sync.Mutex
	hits   []hit
	status int
}

func newReceiver(t *testing.T, secret, token string) *receiver {
	t.Helper()
	r := &receiver{secret: secret, token: token, status: http.StatusOK}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.hits = append(r.hits, hit{req.Header.Clone(), body})
		code := r.status
		r.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) received() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits...)
}

func (r *receiver) setStatus(code int) {
	r.mu.Lock()
	r.status = code
	r.mu.Unlock()
}

// target はこの受け手を指す送り先。
func (r *receiver) target(maxAttempts int) config.Webhook {
	return config.Webhook{
		URL: config.Secret(r.srv.URL + "/" + r.secret), Secret: config.Secret(r.secret), Token: config.Secret(r.token),
		MaxAttempts: maxAttempts, RetryInterval: 30 * time.Second, Timeout: 5 * time.Second,
	}
}

func (r *receiver) secrets() []string {
	return []string{r.secret, r.token, strings.TrimPrefix(r.srv.URL, "http://")}
}

// rig は Notifier と、その台帳・ログ・最終報告の置き場。
type rig struct {
	t      *testing.T
	n      *webhook.Notifier
	dir    string
	state  string
	logs   *testutil.LockedBuffer
	at     time.Time
	cancel context.CancelFunc
	wait   func()
	un     *pane.Unaccepted // Notifier に渡す受理の記録（nil なら渡さない）
}

var noSleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// bareRig は Notifier をまだ作らない rig（台帳を先に置きたいテスト用）。
func bareRig(t *testing.T) *rig {
	t.Helper()
	g := &rig{t: t, dir: t.TempDir(), logs: &testutil.LockedBuffer{}, at: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}
	g.state = filepath.Join(g.dir, "webhook-state.json")
	return g
}

func newRig(t *testing.T, cfg config.Webhook) *rig {
	t.Helper()
	g := bareRig(t)
	g.build(cfg)
	return g
}

// build は台帳から Notifier を作って Start する（再起動の再現にも使う）。
func (g *rig) build(cfg config.Webhook) {
	g.t.Helper()
	n, err := webhook.New(webhook.Options{
		Config: cfg, ReportDir: filepath.Join(g.dir, "reports"), StatePath: g.state, Version: "test",
		Logger: slog.New(slog.NewJSONHandler(g.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Sleep:  noSleep, Unaccepted: g.un,
	})
	if err != nil {
		g.t.Fatal(err)
	}
	g.n = n
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel, g.wait = cancel, n.Start(ctx)
	g.t.Cleanup(func() { cancel(); g.wait() })
}

func (g *rig) logText() string { return g.logs.String() }

func (g *rig) ledger() string {
	b, err := os.ReadFile(g.state)
	if err != nil {
		return ""
	}
	return string(b)
}

func (g *rig) panes(state agent.State, sessions ...string) []pane.PaneState {
	var ps []pane.PaneState
	for i, s := range sessions {
		ps = append(ps, pane.PaneState{
			ID: "%" + string(rune('3'+i)), Session: s, Pos: "T2", Local: "T2", GlobalPos: s + ":T2", State: state,
			Note: &notes.Summary{From: "そら", Repo: "r", Items: []string{"a"}, Next: "n"},
		})
	}
	return ps
}

func (g *rig) snap(state agent.State, sessions ...string) {
	g.t.Helper()
	g.at = g.at.Add(5 * time.Second)
	g.n.Observe(&pane.Snapshot{At: g.at, Panes: g.panes(state, sessions...), ServerStart: "100"})
}

// idleRound はセッションのペインを working にし、最終報告を書いて idle にする。送信が終わるまで待つ。
func (g *rig) idleRound(sessions ...string) {
	g.t.Helper()
	g.snap(agent.StateWorking, sessions...)
	if err := os.MkdirAll(filepath.Join(g.dir, "reports"), 0o700); err != nil {
		g.t.Fatal(err)
	}
	for _, s := range sessions {
		if err := os.WriteFile(filepath.Join(g.dir, "reports", s+"_T2.md"), []byte("done\n"), 0o600); err != nil {
			g.t.Fatal(err)
		}
	}
	g.snap(agent.StateIdle, sessions...)
	for range 5 { // idle のまま続けても、重ねて送られない
		g.snap(agent.StateIdle, sessions...)
	}
	g.drain()
}

// drain は進行中の送信が終わるまで待つ（終了処理を呼び、同じ台帳で作り直す）。
func (g *rig) drain() {
	g.t.Helper()
	testutil.Eventually(g.t, "進行中の送信が終わる（台帳の outbox が空になる）", func() bool {
		return strings.Contains(g.ledger(), `"outbox":{}`)
	})
	g.cancel()
	g.wait()
}

func payloadOf(t *testing.T, h hit) webhook.Payload {
	t.Helper()
	var pl webhook.Payload
	if err := json.Unmarshal(h.body, &pl); err != nil {
		t.Fatal(err)
	}
	return pl
}

func assertSigned(t *testing.T, r *receiver, h hit) {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(r.secret))
	mac.Write(h.body)
	if got, want := h.header.Get("X-Swarm-Console-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Error("署名がそのセッションの鍵と合いません")
	}
	if got := h.header.Get("Authorization"); got != "Bearer "+r.token {
		t.Error("Authorization がそのセッションのトークンと違います")
	}
}

// 秘密の値（URL・鍵・トークン）が、ログにも台帳にも出ていないこと。
func (g *rig) assertNoSecrets(rs ...*receiver) {
	g.t.Helper()
	out := g.logText() + "\n" + g.ledger()
	for _, r := range rs {
		for _, s := range r.secrets() {
			if strings.Contains(out, s) {
				g.t.Errorf("%q が出ています:\n%s", s, out)
			}
		}
	}
}
