package server_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

var viewTools = []string{
	"report", "sessions_list", "panes_status", "pane_tail", "ask_card", "report_read", "host_status", "agent_auth_status", "note_get", "note_history",
}

var opsTools = []string{"ask_pick", "send_text", "send_escape", "pane_start", "pane_close", "dispatch", "note_set", "note_log", "file_get"}

func sorted(s []string) []string { s = slices.Clone(s); slices.Sort(s); return s }

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tool := range res.Tools {
		out = append(out, tool.Name)
	}
	return out
}

// /report の tools/list は 10 個で操作を含まず、/ops は 19 個（FR-SAFE-6・NFR-SEC-10）。
func TestToolsListPerEndpoint(t *testing.T) {
	e := newEnv(t)
	got := toolNames(t, e.connect(t, "/report", "viewer"))
	if !slices.Equal(sorted(got), sorted(viewTools)) {
		t.Errorf("/report のツール = %v", got)
	}
	for _, n := range opsTools {
		if slices.Contains(got, n) {
			t.Errorf("/report に操作ツール %s が載っています", n)
		}
	}
	if got := toolNames(t, e.connect(t, "/report", "boss")); len(got) != 10 {
		t.Errorf("ops トークンの /report のツール数 = %d, want 10", len(got))
	}
	got = toolNames(t, e.connect(t, "/ops", "boss"))
	if !slices.Equal(sorted(got), sorted(append(slices.Clone(viewTools), opsTools...))) || len(got) != 19 {
		t.Errorf("/ops のツール = %v", got)
	}
}

func TestToolSchemasPublished(t *testing.T) {
	e := newEnv(t)
	res, err := e.connect(t, "/ops", "boss").ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(b), `"additionalProperties":false`) || tool.Description == "" {
			t.Errorf("%s: schema=%s desc=%q", tool.Name, b, tool.Description)
		}
	}
}

// /report で操作ツールを tools/call すると unknown_tool で拒否する。何も実行されない。
func TestReportEndpointRejectsOpsTools(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, "/report", "boss")
	for _, name := range append(slices.Clone(opsTools), "nonexistent") {
		text, isErr := callText(t, cs, name, map[string]any{"pane": "T1", "text": "x", "confirm": true, "n": 1})
		if !isErr || text != `{"error":{"code":"unknown_tool","message":"この窓口にないツールです"}}` {
			t.Errorf("%s: isError=%v %s", name, isErr, text)
		}
	}
	if e.f.Ops.Called() {
		t.Error("/report から操作が実行されました")
	}
	if _, ok := e.f.Notes.Get("%1"); ok {
		t.Error("/report からメモが書かれました")
	}
}

func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: content が %d 個", name, len(res.Content))
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestToolCallResultAndError(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, "/ops", "boss")
	text, isErr := callText(t, cs, "pane_tail", map[string]any{"pane": "t1", "lines": 5})
	if isErr || text != `{"pane":"T1","lines":5,"text":"line1\nline2","truncated":false}` {
		t.Errorf("pane_tail = %v %s", isErr, text)
	}
	// ツールの失敗は isError: true ＋ 固定形の本文（API 仕様 §3.1）。
	text, isErr = callText(t, cs, "pane_tail", map[string]any{"pane": "Z9"})
	if !isErr || !strings.HasPrefix(text, `{"error":{"code":"pane_not_found","message":"ペインが見つかりません"`) {
		t.Errorf("pane_not_found = %v %s", isErr, text)
	}
	// 未知の引数キーは invalid_args（二重の防御）。
	text, isErr = callText(t, cs, "panes_status", map[string]any{"bogus": 1})
	if !isErr || !strings.Contains(text, `"code":"invalid_args"`) || !strings.Contains(text, `"field":"bogus"`) {
		t.Errorf("未知キー = %v %s", isErr, text)
	}
	text, isErr = callText(t, cs, "send_escape", map[string]any{"pane": "B2"})
	if !isErr || !strings.Contains(text, `"code":"confirm_required"`) || e.f.Ops.Called() {
		t.Errorf("confirm なし = %v %s", isErr, text)
	}
	// report の markdown は本文そのまま（JSON に包まない）。
	text, isErr = callText(t, cs, "report", nil)
	if isErr || !strings.HasPrefix(text, "# ") || strings.HasPrefix(text, "{") {
		t.Errorf("report = %v %.60q", isErr, text)
	}
}

func TestOpsToolReachesOps(t *testing.T) {
	e := newEnv(t)
	e.f.Ops.EscapeRes = pane.EscapeResult{OK: true, State: agent.StateIdle, Interrupted: true}
	text, isErr := callText(t, e.connect(t, "/ops", "boss"), "send_escape", map[string]any{"pane": "B2", "confirm": true})
	if isErr || text != `{"ok":true,"state":"idle","interrupted":true}` || e.f.Ops.LastRef.ID != "%4" {
		t.Errorf("send_escape = %v %s ref=%+v", isErr, text, e.f.Ops.LastRef)
	}
}
