package tooltests_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

var viewTools = []string{
	"report", "sessions_list", "panes_status", "pane_tail", "ask_card", "report_read", "host_status", "agent_auth_status", "note_get", "note_history",
}

var opsTools = []string{
	"ask_pick", "send_text", "send_escape", "pane_start", "pane_close", "dispatch", "note_set", "note_log", "file_get",
}

func names(ts []tools.Tool) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

func TestEndpointToolSets(t *testing.T) {
	f := toolstest.New(t)
	if got := names(f.Tools.ForEndpoint(tools.EndpointReport)); !slices.Equal(got, viewTools) {
		t.Errorf("/report のツール = %v, want %v", got, viewTools)
	}
	if got := names(f.Tools.ForEndpoint(tools.EndpointOps)); !slices.Equal(got, append(slices.Clone(viewTools), opsTools...)) {
		t.Errorf("/ops のツール = %v", got)
	}
	if got := f.Tools.ForEndpoint("/other"); got != nil {
		t.Errorf("未知の窓口にツールが出ています: %v", names(got))
	}
	for _, n := range opsTools {
		if _, ok := f.Tools.Lookup(tools.EndpointReport, n); ok {
			t.Errorf("/report から操作ツール %s を引けてしまいます", n)
		}
		if _, ok := f.Tools.Lookup(tools.EndpointOps, n); !ok {
			t.Errorf("/ops から %s を引けません", n)
		}
	}
}

func TestToolDefinitions(t *testing.T) {
	f := toolstest.New(t)
	for _, tool := range f.Tools.ForEndpoint(tools.EndpointOps) {
		s, ok := tool.Schema.(map[string]any)
		if !ok || s["type"] != "object" || s["additionalProperties"] != false {
			t.Errorf("%s: スキーマが additionalProperties:false の object ではありません: %v", tool.Name, tool.Schema)
		}
		if tool.Description == "" || tool.Handler == nil {
			t.Errorf("%s: 説明かハンドラがありません", tool.Name)
		}
		if _, err := json.Marshal(tool.Schema); err != nil {
			t.Errorf("%s: スキーマを JSON にできません: %v", tool.Name, err)
		}
	}
	desc := func(name string) string {
		tool, _ := f.Tools.Lookup(tools.EndpointOps, name)
		return tool.Description
	}
	if !strings.Contains(desc("send_escape"), "オーナーの OK があるときだけ使う") {
		t.Error("send_escape の説明に OK の条件がありません")
	}
	if !strings.Contains(desc("report"), "そのまま表示する") {
		t.Error("report の説明に「そのまま表示する」がありません")
	}
}

// どのツールも未知のキーは invalid_args（details.field に引数名だけ）。大文字小文字違いのキーも未知として扱う。
func TestEveryToolRejectsUnknownKeys(t *testing.T) {
	f := toolstest.New(t)
	for _, name := range slices.Sorted(maps.Keys(toolSet(f))) {
		for _, args := range []string{`{"bogus":1}`, `{"PANE":"T1"}`, `{"Pane":"T1","bogus":true}`} {
			_, err := call(t, f, name, args)
			d := wantCode(t, err, "invalid_args")
			if field, _ := d["field"].(string); !strings.EqualFold(field, "bogus") && !strings.EqualFold(field, "pane") {
				t.Errorf("%s(%s): details.field = %v", name, args, d)
			}
		}
		for _, args := range []string{`[]`, `"x"`, `{"a":1} {}`, `7`} {
			_, err := call(t, f, name, args)
			wantCode(t, err, "invalid_args")
		}
	}
	if f.Ops.Called() {
		t.Error("引数不正なのに操作が呼ばれました")
	}
}

func toolSet(f *toolstest.Fixture) map[string]bool {
	m := map[string]bool{}
	for _, tool := range f.Tools.ForEndpoint(tools.EndpointOps) {
		m[tool.Name] = true
	}
	return m
}

func TestNewRequiresDeps(t *testing.T) {
	f := toolstest.New(t)
	d := f.Deps
	d.Ops = nil
	if _, err := tools.New(d); err == nil {
		t.Error("Ops なしで組み立てられてしまいます")
	}
}
