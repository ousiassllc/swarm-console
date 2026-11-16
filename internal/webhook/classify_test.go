package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/config"
)

// 再送して意味がある失敗だけ true。リクエストを作れない（reasonRequest）・設定の誤り（404・302）は再送しない。
func TestRetryable(t *testing.T) {
	for _, c := range []struct {
		status int
		reason string
		want   bool
	}{
		{http.StatusRequestTimeout, reasonStatus, true},
		{http.StatusTooEarly, reasonStatus, true},
		{http.StatusTooManyRequests, reasonStatus, true},
		{http.StatusInternalServerError, reasonStatus, true},
		{http.StatusServiceUnavailable, reasonStatus, true},
		{http.StatusNotFound, reasonStatus, false},
		{http.StatusUnauthorized, reasonStatus, false},
		{http.StatusFound, reasonStatus, false},
		{0, reasonConnect, true},
		{0, reasonDNS, true},
		{0, reasonTimeout, true},
		{0, reasonTLS, true},
		{0, reasonRequest, false},
		{0, reasonCanceled, false},
		{0, "unknown", false},
	} {
		if got := retryable(c.status, c.reason); got != c.want {
			t.Errorf("retryable(%d, %q) = %v, want %v", c.status, c.reason, got, c.want)
		}
	}
}

// get は client で url に GET して、通信のエラーを返す（応答は見ない）。
func get(t *testing.T, client *http.Client, url string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

// classify は、実際の通信の失敗を URL・ホスト名を含まない固定の理由にする。
func TestClassify(t *testing.T) {
	t.Run("TLS（信頼できない証明書）", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer srv.Close()
		if got := classify(get(t, newClient(5*time.Second), srv.URL)); got != reasonTLS { // 既定の信頼先に、テスト用の証明書は無い
			t.Errorf("classify = %q, want %q", got, reasonTLS)
		}
	})
	t.Run("タイムアウト", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })) // クライアントが切るまで返さない
		defer srv.Close()
		if got := classify(get(t, newClient(100*time.Millisecond), srv.URL)); got != reasonTimeout {
			t.Errorf("classify = %q, want %q", got, reasonTimeout)
		}
	})
	t.Run("接続できない", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		if got := classify(get(t, newClient(5*time.Second), url)); got != reasonConnect {
			t.Errorf("classify = %q, want %q", got, reasonConnect)
		}
	})
	t.Run("名前解決", func(t *testing.T) {
		// 実際の名前解決は環境によって結果が違うので、エラーを組み立てて確かめる。
		err := &url.Error{Op: "Post", URL: "https://swarm-console-test.invalid/", Err: &net.DNSError{Err: "no such host", Name: "swarm-console-test.invalid", IsNotFound: true}}
		if got := classify(err); got != reasonDNS {
			t.Errorf("classify = %q, want %q", got, reasonDNS)
		}
	})
	t.Run("取り消し", func(t *testing.T) {
		if got := classify(fmt.Errorf("x: %w", context.Canceled)); got != reasonCanceled {
			t.Errorf("canceled: %q", got)
		}
		if got := classify(context.DeadlineExceeded); got != reasonTimeout {
			t.Errorf("deadline: %q", got)
		}
	})
}

// 応答なしで切れた（EOF）・接続のリセット・分類できない通信の失敗は、一時的な失敗として扱う（reasonConnect。再送する）。
func TestClassifyTransportFailuresAreRetryable(t *testing.T) {
	for name, err := range map[string]error{
		"EOF":                   &url.Error{Op: "Post", URL: "https://x/", Err: io.EOF},
		"ErrUnexpectedEOF":      io.ErrUnexpectedEOF,
		"ECONNRESET(OpError)":   &url.Error{Op: "Post", URL: "https://x/", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		"ECONNRESET(url.Error)": &url.Error{Op: "Post", URL: "https://x/", Err: syscall.ECONNRESET},
		"分類できない":                errors.New("unsupported protocol scheme"),
	} {
		got := classify(err)
		if got != reasonConnect || !retryable(0, got) {
			t.Errorf("%s: classify = %q（retryable=%v）, want %q で再送する", name, got, retryable(0, got), reasonConnect)
		}
	}
}

// 応答なしで接続を切られた（EOF）ときは再送し、次の試行で届く。ちょうど 2 回の POST。
func TestEOFWithoutResponseIsRetriedAndDelivered(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	h.mu.Lock()
	h.intercept = func(w http.ResponseWriter, _ *http.Request) bool {
		if calls.Add(1) > 1 {
			return false
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return true
	}
	h.mu.Unlock()
	h.setStatus(http.StatusNoContent)
	h.start()
	h.idleWithReport()
	if got := len(h.received()); got != 2 {
		t.Fatalf("POST = %d 回, want 2", got)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "webhook を送りました") || strings.Contains(logs, `"level":"ERROR"`) {
		t.Errorf("届いたはずです:\n%s", logs)
	}
	if !strings.Contains(logs, `"reason":"connect"`) || len(h.n.outbox) != 0 {
		t.Errorf("1 回目の失敗の理由・outbox が違います: outbox=%d\n%s", len(h.n.outbox), logs)
	}
}

// リクエストを作れない（URL の誤り）だけは、再送せずに諦める（reasonRequest）。
func TestUnbuildableRequestIsNotRetried(t *testing.T) {
	h := newHarness(t)
	h.cfg.URL = config.Secret("https://bad host/\x7f")
	h.start()
	h.idleWithReport()
	if len(h.received()) != 0 || len(h.sleeps()) != 0 {
		t.Fatalf("POST = %d 回, 待ち = %d 回, want 0・0", len(h.received()), len(h.sleeps()))
	}
	if logs := h.logs.String(); !strings.Contains(logs, `"reason":"request"`) || !strings.Contains(logs, `"attempts":1`) {
		t.Errorf("理由・回数がログにありません:\n%s", logs)
	}
}

// Notifier を fmt・slog に渡しても、設定（URL・鍵・トークン）は出ない。
func TestNotifierPrintsNoConfig(t *testing.T) {
	h := newHarness(t)
	h.build()
	var logs strings.Builder
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("notifier", "n", h.n)
	out := fmt.Sprintf("%v|%+v|%s|%#v\n%s", h.n, h.n, h.n, h.n, logs.String())
	for _, secret := range []string{secretToken, secretKey, secretPath, strings.TrimPrefix(h.srv.URL, "http://")} {
		if strings.Contains(out, secret) {
			t.Errorf("%q が出ています:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "webhook.Notifier") {
		t.Errorf("固定の文がありません:\n%s", out)
	}
}
