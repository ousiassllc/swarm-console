package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/server"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

const (
	body401 = `{"error":{"code":"unauthorized","message":"認証に失敗しました"}}`
	body403 = `{"error":{"code":"forbidden_scope","message":"このトークンではこの窓口を使えません"}}`
)

// 401 はどの失敗でも同じ応答（トークンの有無・失効のヒントを出さない）。
func TestUnauthorized(t *testing.T) {
	e := newEnv(t)
	if err := auth.Revoke(e.tokPath, "viewer", toolstest.At); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, header string }{
		{"なし", ""},
		{"Bearer でない", "Basic " + e.tokens["boss"]},
		{"不一致", "Bearer sc_" + strings.Repeat("x", 43)},
		{"失効済み", "Bearer " + e.tokens["viewer"]},
		{"スキームだけ", "Bearer"},
	} {
		for _, path := range []string{"/report", "/ops"} {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, e.ts.URL+path, strings.NewReader(initBody))
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			resp, body := e.do(t, req)
			if resp.StatusCode != 401 || body != body401 || resp.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("%s %s: %d %q WWW-Authenticate=%q", c.name, path, resp.StatusCode, body, resp.Header.Get("WWW-Authenticate"))
			}
		}
	}
}

func TestForbiddenScope(t *testing.T) {
	e := newEnv(t)
	resp, body := e.post(t, http.MethodPost, "/ops", e.tokens["viewer"], initBody)
	if resp.StatusCode != 403 || body != body403 {
		t.Errorf("report トークンで /ops: %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("WWW-Authenticate") != "" {
		t.Error("403 に WWW-Authenticate が付いています")
	}
	for _, name := range []string{"viewer", "boss"} {
		if resp, body := e.post(t, http.MethodPost, "/report", e.tokens[name], initBody); resp.StatusCode != 200 {
			t.Errorf("%s で /report: %d %s", name, resp.StatusCode, body)
		}
	}
	if resp, body := e.post(t, http.MethodPost, "/ops", e.tokens["boss"], initBody); resp.StatusCode != 200 {
		t.Errorf("ops トークンで /ops: %d %s", resp.StatusCode, body)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/", "/other", "/ops/", "/report/x", "/OPS"} {
		for _, token := range []string{"", e.tokens["boss"]} {
			resp, body := e.post(t, http.MethodPost, path, token, "{}")
			if resp.StatusCode != 404 || body != `{"error":{"code":"not_found","message":"見つかりません"}}` {
				t.Errorf("POST %s: %d %s", path, resp.StatusCode, body)
			}
		}
	}
	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodPatch} {
		for _, path := range []string{"/report", "/ops"} {
			resp, body := e.post(t, m, path, e.tokens["boss"], "")
			if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" ||
				body != `{"error":{"code":"method_not_allowed","message":"このメソッドは使えません"}}` {
				t.Errorf("%s %s: %d %s", m, path, resp.StatusCode, body)
			}
		}
	}
	// 認証が先: トークンなしの GET は 405 ではなく 401。
	if resp, _ := e.post(t, http.MethodGet, "/ops", "", ""); resp.StatusCode != 401 {
		t.Errorf("トークンなしの GET /ops = %d, want 401", resp.StatusCode)
	}
}

func TestPayloadTooLarge(t *testing.T) {
	e := newEnv(t)
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("a", server.MaxBodyBytes) + `"}}`
	const want = `{"error":{"code":"payload_too_large","message":"リクエスト本文が大きすぎます"}}`
	// Content-Length で分かる場合。
	if resp, body := e.post(t, http.MethodPost, "/ops", e.tokens["boss"], big); resp.StatusCode != 413 || body != want {
		t.Errorf("Content-Length 超過: %d %s", resp.StatusCode, body)
	}
	// 長さ不明（chunked）でも、読んだ時点で上限を超えたら 413。
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, e.ts.URL+"/ops", strings.NewReader(big))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+e.tokens["boss"])
	resp, body := e.do(t, req)
	if resp.StatusCode != 413 || body != want {
		t.Errorf("chunked 超過: %d %s", resp.StatusCode, body)
	}
	// 認証前に本文を読まない: トークンなしは 401。
	if resp, _ := e.post(t, http.MethodPost, "/ops", "", big); resp.StatusCode != 401 {
		t.Errorf("トークンなし + 巨大本文 = %d, want 401", resp.StatusCode)
	}
	// ちょうど上限以内なら通る（JSON-RPC として不正でも 413 にならない）。
	if resp, _ := e.post(t, http.MethodPost, "/ops", e.tokens["boss"], strings.Repeat(" ", server.MaxBodyBytes)); resp.StatusCode == 413 {
		t.Error("上限ちょうどの本文が 413 になりました")
	}
}

// P-02: ステートレス。Mcp-Session-Id を発行せず、JSON で応答する。
func TestStateless(t *testing.T) {
	e := newEnv(t)
	resp, body := e.post(t, http.MethodPost, "/ops", e.tokens["boss"], initBody)
	if resp.StatusCode != 200 || resp.Header.Get("Mcp-Session-Id") != "" {
		t.Errorf("initialize: %d session=%q", resp.StatusCode, resp.Header.Get("Mcp-Session-Id"))
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || !strings.Contains(body, `"serverInfo"`) {
		t.Errorf("応答 = %s %s", resp.Header.Get("Content-Type"), body)
	}
	// initialize なしでも tools/list が通る（セッションを持たない）。
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	if resp, body := e.post(t, http.MethodPost, "/report", e.tokens["viewer"], list); resp.StatusCode != 200 || !strings.Contains(body, `"panes_status"`) {
		t.Errorf("セッションなしの tools/list: %d %s", resp.StatusCode, body)
	}
}
