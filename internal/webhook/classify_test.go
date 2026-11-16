package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		err := get(t, newClient(5*time.Second), "http://swarm-console-test.invalid/")
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) {
			t.Skipf("この環境では名前解決の失敗が DNSError にならない: %T", errors.Unwrap(err))
		}
		if got := classify(err); got != reasonDNS {
			t.Errorf("classify = %q, want %q", got, reasonDNS)
		}
	})
	t.Run("取り消し・そのほか", func(t *testing.T) {
		if got := classify(fmt.Errorf("x: %w", context.Canceled)); got != reasonCanceled {
			t.Errorf("canceled: %q", got)
		}
		if got := classify(context.DeadlineExceeded); got != reasonTimeout {
			t.Errorf("deadline: %q", got)
		}
		if got := classify(errors.New("unsupported protocol scheme")); got != reasonRequest {
			t.Errorf("そのほか: %q", got)
		}
	})
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
