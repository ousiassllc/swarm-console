package server_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/mcpapi"
	"github.com/ousiassllc/swarm-console/internal/server"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// syncBuffer はログの出力先（並行に書かれるので排他する）。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// env は 1 テストぶんのサーバー一式。
type env struct {
	f       *toolstest.Fixture
	srv     *server.Server
	ts      *httptest.Server
	logs    *syncBuffer
	tokens  map[string]string // 名前 → 平文
	tokPath string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	f := toolstest.New(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	tokPath := filepath.Join(t.TempDir(), "tokens.toml")
	e := &env{f: f, logs: logs, tokens: map[string]string{}, tokPath: tokPath}
	e.addToken(t, "viewer", auth.ScopeReport)
	e.addToken(t, "boss", auth.ScopeOps)
	a, err := auth.Open(tokPath, clock.Real{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := mcpapi.Options{Tools: f.Tools, Version: "test", Audit: server.AuditFunc(logger)}
	e.srv, err = server.New(server.Options{
		Auth: a, Logger: logger,
		Report: mcpapi.NewHTTPHandler(mcpapi.NewReportServer(o)),
		Ops:    mcpapi.NewHTTPHandler(mcpapi.NewOpsServer(o)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.ts = httptest.NewServer(e.srv.Handler())
	t.Cleanup(e.ts.Close)
	return e
}

func (e *env) addToken(t *testing.T, name string, scope auth.Scope) {
	t.Helper()
	plain, err := auth.Add(e.tokPath, name, scope, toolstest.At)
	if err != nil {
		t.Fatal(err)
	}
	e.tokens[name] = plain
}

// post は生の POST。token が "" なら Authorization なし。
func (e *env) post(t *testing.T, method, path, token, body string) (*result, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return e.do(t, req)
}

// do は req を送って状態コード・ヘッダー・本文を返す（本文は閉じる）。
func (e *env) do(t *testing.T, req *http.Request) (*result, string) {
	t.Helper()
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return &result{StatusCode: resp.StatusCode, Header: resp.Header}, string(b)
}

// result は応答の状態コードとヘッダー（本文は別に返す）。
type result struct {
	StatusCode int
	Header     http.Header
}

// bearer は Authorization を付ける RoundTripper。
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// connect は SDK のクライアントで窓口に接続する。
func (e *env) connect(t *testing.T, path, tokenName string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             e.ts.URL + path,
		HTTPClient:           &http.Client{Transport: bearer{e.tokens[tokenName]}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("%s に接続できません: %v", path, err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// listen は ループバックの空きポートで待ち受ける（noctx 対応）。
func listen(t *testing.T) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// canDial は addr に繋がるか。
func canDial(addr string) bool {
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
