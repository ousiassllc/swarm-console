package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTimeout は 1 回の MCP 呼び出しの上限（pane_close など数秒かかる操作がある）。
const callTimeout = 120 * time.Second

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// Client は 1 つの窓口への独立した MCP 接続（別トークン・別セッション）。
type Client struct {
	T        testing.TB
	Name     string // トークン名
	Endpoint string // /report か /ops
	CS       *mcp.ClientSession
}

// Connect は endpoint に token（トークン名）で MCP 接続する。
func (h *H) Connect(endpoint, tokenName string) *Client {
	h.T.Helper()
	c, err := h.TryConnect(endpoint, h.tokens[tokenName])
	if err != nil {
		h.T.Fatalf("%s に %s で接続できません: %v", endpoint, tokenName, err)
	}
	c.Name = tokenName
	h.mu.Lock()
	h.all = append(h.all, c)
	h.mu.Unlock()
	return c
}

// TryConnect は平文のトークンで接続する。失敗はエラーで返す（401・403 の確認用）。
func (h *H) TryConnect(endpoint, token string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "integration-" + endpoint, Version: "1"}, nil)
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://" + h.Addr + endpoint, HTTPClient: &http.Client{Transport: bearerTransport{token}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		return nil, err
	}
	return &Client{T: h.T, Endpoint: endpoint, CS: cs}, nil
}

func (h *H) cached(endpoint, name string) *Client {
	h.T.Helper()
	key := endpoint + "|" + name
	h.mu.Lock()
	c := h.clients[key]
	h.mu.Unlock()
	if c != nil {
		return c
	}
	c = h.Connect(endpoint, name)
	h.mu.Lock()
	h.clients[key] = c
	h.mu.Unlock()
	return c
}

// Ops は /ops への ops-a トークンの接続（テスト中は使い回す）。
func (h *H) Ops() *Client { h.T.Helper(); return h.cached("/ops", OpsA) }

// OpsB は /ops への ops-b トークンの接続（Ops とは別のセッション）。
func (h *H) OpsB() *Client { h.T.Helper(); return h.cached("/ops", OpsB) }

// Report は /report への report-a トークンの接続。
func (h *H) Report() *Client { h.T.Helper(); return h.cached("/report", ReportA) }

// ReportB は /report への report-b トークンの接続。
func (h *H) ReportB() *Client { h.T.Helper(); return h.cached("/report", ReportB) }

// Result はツール呼び出しの結果。
type Result struct {
	Text    string
	IsError bool
}

// Call はツールを呼ぶ。JSON-RPC レベルの失敗はテストを止める（ツールの失敗は IsError で返る）。
func (c *Client) Call(name string, args map[string]any) Result {
	c.T.Helper()
	r, err := c.TryCall(name, args)
	if err != nil {
		c.T.Fatalf("%s %s: %v", c.Endpoint, name, err)
	}
	return r
}

// TryCall は Call のエラー返し版（goroutine から呼ぶ・RPC エラーを確かめる用）。
func (c *Client) TryCall(name string, args map[string]any) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if args == nil {
		args = map[string]any{}
	}
	res, err := c.CS.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return Result{}, err
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	return Result{Text: text, IsError: res.IsError}, nil
}

// Must はツールを呼び、成功（isError でない）でなければテストを止める。
func (c *Client) Must(name string, args map[string]any) Result {
	c.T.Helper()
	r := c.Call(name, args)
	if r.IsError {
		c.T.Fatalf("%s %s %v が失敗: %s", c.Endpoint, name, args, r.Text)
	}
	return r
}

// Map はツールを成功させて、結果の JSON をマップで返す。
func (c *Client) Map(name string, args map[string]any) map[string]any {
	c.T.Helper()
	return c.Must(name, args).Map(c.T)
}

// Map は結果の JSON をマップにする。
func (r Result) Map(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Text), &m); err != nil {
		t.Fatalf("結果が JSON ではありません: %v\n%s", err, r.Text)
	}
	return m
}

// Decode は結果の JSON を v に読む。
func (r Result) Decode(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Text), v); err != nil {
		t.Fatalf("結果を読めません: %v\n%s", err, r.Text)
	}
}

// ErrorBody はツールの失敗の本文（API 仕様 §3.1）。
type ErrorBody struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

// Err は失敗の結果から本文を読む。失敗でなければテストを止める。
func (r Result) Err(t testing.TB) ErrorBody {
	t.Helper()
	if !r.IsError {
		t.Fatalf("失敗のはずが成功: %s", r.Text)
	}
	var e ErrorBody
	if err := json.Unmarshal([]byte(r.Text), &e); err != nil {
		t.Fatalf("失敗の本文が JSON ではありません: %v\n%s", err, r.Text)
	}
	return e
}

// Code は失敗のエラーコード。
func (r Result) Code(t testing.TB) string {
	t.Helper()
	return r.Err(t).Error.Code
}
