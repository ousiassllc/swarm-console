package integration

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

func wantBody(code apperr.Code) string {
	return `{"error":{"code":"` + string(code) + `","message":"` + apperr.Message(code) + `"}}`
}

// 認証なし・違うトークン・失効・形式違いは、窓口によらず同じ 401 と同じ本文（理由を漏らさない）。窓口違いは 403。
func TestHTTPAuthRejections(t *testing.T) {
	h := harness.Start(t, harness.Options{NoSampler: true})
	revoked, err := auth.Add(h.TokensPath(), "revoked-one", auth.ScopeOps, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Revoke(h.TokensPath(), "revoked-one", time.Now()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, endpoint, authz string
		status                int
		code                  apperr.Code
	}{
		{"/ops 認証なし", "/ops", "", 401, apperr.Unauthorized},
		{"/report 認証なし", "/report", "", 401, apperr.Unauthorized},
		{"/ops 違うトークン", "/ops", "Bearer sc_" + strings.Repeat("x", 43), 401, apperr.Unauthorized},
		{"/report 違うトークン", "/report", "Bearer sc_" + strings.Repeat("x", 43), 401, apperr.Unauthorized},
		{"/ops 失効", "/ops", "Bearer " + revoked, 401, apperr.Unauthorized},
		{"/report 失効", "/report", "Bearer " + revoked, 401, apperr.Unauthorized},
		{"/ops Bearer でない", "/ops", "Basic " + h.Token(harness.OpsA), 401, apperr.Unauthorized},
		{"/ops トークンだけ", "/ops", h.Token(harness.OpsA), 401, apperr.Unauthorized},
		{"report トークンで /ops", "/ops", "Bearer " + h.Token(harness.ReportA), 403, apperr.ForbiddenScope},
		{"もう 1 つの report トークンで /ops", "/ops", "Bearer " + h.Token(harness.ReportB), 403, apperr.ForbiddenScope},
		{"未知のパス", "/nope", "Bearer " + h.Token(harness.OpsA), 404, apperr.NotFound},
		{"未知のパス（認証なし）", "/nope", "", 404, apperr.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := h.Post(tt.endpoint, tt.authz, initBody)
			if status != tt.status || body != wantBody(tt.code) {
				t.Errorf("status=%d body=%s, want %d %s", status, body, tt.status, wantBody(tt.code))
			}
		})
	}
	// 通るもの: ops トークンは /report も通る（P-03）。
	for _, c := range []struct{ endpoint, token string }{{"/report", harness.ReportA}, {"/report", harness.OpsA}, {"/ops", harness.OpsB}} {
		if status, _ := h.Post(c.endpoint, "Bearer "+h.Token(c.token), initBody); status != 200 {
			t.Errorf("%s を %s で呼ぶと %d", c.endpoint, c.token, status)
		}
	}
	// MCP クライアントの接続も同じ理由で拒否される。
	if _, err := h.TryConnect("/ops", "sc_wrong"); err == nil {
		t.Error("違うトークンで MCP 接続できた")
	}
	if _, err := h.TryConnect("/ops", h.Token(harness.ReportA)); err == nil {
		t.Error("report トークンで /ops に MCP 接続できた")
	}
	// ここまでの拒否でトークンの平文が応答・ログに出ていないこと（Stop 時にもログを確かめる）。
	if strings.Contains(h.AuditLog(), revoked) {
		t.Error("ログに失効トークンの平文が出ている")
	}
}

func TestHTTPMethodAndBodyLimits(t *testing.T) {
	h := harness.Start(t, harness.Options{NoSampler: true})
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, err := http.NewRequestWithContext(context.Background(), method, "http://"+h.Addr+"/ops", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+h.Token(harness.OpsA))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
			t.Errorf("%s: %d Allow=%q", method, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"report","arguments":{"x":"` + strings.Repeat("a", 256<<10) + `"}}}`
	if status, body := h.Post("/ops", "Bearer "+h.Token(harness.OpsA), big); status != 413 || body != wantBody(apperr.PayloadTooLarge) {
		t.Errorf("大きな本文: %d %s", status, body)
	}
}

// AC-2: /report の tools/list は見るツール 9 つだけで、操作を含まない。/ops は 17。tools/call で操作を呼んでも何も実行しない。
func TestToolListsAndScopes(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	ctx := context.Background()
	names := func(c *harness.Client) []string {
		res, err := c.CS.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tool := range res.Tools {
			out = append(out, tool.Name)
		}
		slices.Sort(out)
		return out
	}
	viewTools := []string{"agent_auth_status", "ask_card", "host_status", "note_get", "note_history", "pane_tail", "panes_status", "report", "report_read"}
	opsTools := []string{"ask_pick", "dispatch", "note_log", "note_set", "pane_close", "pane_start", "send_escape", "send_text"}
	reportClients := []*harness.Client{h.Report(), h.ReportB(), h.Connect("/report", harness.OpsA)}
	for _, c := range reportClients {
		if got := names(c); !slices.Equal(got, viewTools) {
			t.Errorf("/report（%s）の tools/list = %v", c.Name, got)
		}
	}
	all := append(slices.Clone(viewTools), opsTools...)
	slices.Sort(all)
	for _, c := range []*harness.Client{h.Ops(), h.OpsB()} {
		if got := names(c); !slices.Equal(got, all) || len(got) != 17 {
			t.Errorf("/ops（%s）の tools/list = %v", c.Name, got)
		}
	}

	// 操作のツールを /report で呼ぶ → 同じ unknown_tool。ペインには何も届かない。
	id := h.NewAgent("omp", "idle")
	h.WaitState(id, "idle")
	args := map[string]map[string]any{
		"send_text": {"pane": id, "text": "x"}, "send_escape": {"pane": id, "confirm": true}, "ask_pick": {"pane": id, "n": 1},
		"pane_close": {"pane": id}, "note_set": {"pane": id, "set": map[string]any{"from": "x"}}, "note_log": {"pane": id, "text": "x"},
		"pane_start": {"pane": id, "agent": "omp", "cwd": h.Work, "title": "t"},
		"dispatch":   {"pane": id, "agent": "omp", "task": "t", "profile": "fix"},
	}
	for _, tool := range opsTools {
		var first string
		for _, c := range reportClients {
			r := c.Call(tool, args[tool])
			if code := r.Code(t); code != "unknown_tool" {
				t.Errorf("/report %s（%s）: %s", tool, c.Name, r.Text)
			}
			if first == "" {
				first = r.Text
			} else if r.Text != first {
				t.Errorf("%s の応答がクライアントで違う:\n%s\n%s", tool, first, r.Text)
			}
		}
	}
	if ev := h.Events(id); len(ev) != 0 {
		t.Errorf("/report の拒否のあとにペインへ何か届いた: %+v", ev)
	}
	if note := h.Ops().Map("note_get", map[string]any{"pane": id})["note"]; note != nil {
		t.Errorf("/report の note_set が効いている: %v", note)
	}
	// 本当に存在しないツール名も同じ。
	if code := h.Report().Call("no_such_tool", nil).Code(t); code != "unknown_tool" {
		t.Errorf("存在しないツール: %s", code)
	}
	// resources は /report でも全部読める（読み取りのみ）。
	if _, err := h.Report().CS.ReadResource(ctx, &mcp.ReadResourceParams{URI: "swarm://report"}); err != nil {
		t.Errorf("/report の resource: %v", err)
	}
}
