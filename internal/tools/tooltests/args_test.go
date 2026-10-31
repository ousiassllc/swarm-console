package tooltests_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// 引数の型・必須・範囲・排他の違反はすべて invalid_args で、操作は呼ばれない。
func TestInvalidArgs(t *testing.T) {
	long := strings.Repeat("あ", 81)
	cases := []struct {
		tool, args, field string
	}{
		{"pane_tail", `{}`, "pane"},
		{"pane_tail", `{"pane":""}`, "pane"},
		{"pane_tail", `{"pane":5}`, "pane"},
		{"pane_tail", `{"pane":"T1","lines":0}`, "lines"},
		{"pane_tail", `{"pane":"T1","lines":201}`, "lines"},
		{"pane_tail", `{"pane":"T1","lines":"5"}`, "lines"},
		{"pane_tail", `{"pane":"T1","lines":3.5}`, "lines"},
		{"pane_tail", `{"pane":"T1","raw":"yes"}`, "raw"},
		{"panes_status", `{"session":"nope"}`, "session"},
		{"panes_status", `{"session":1}`, "session"},
		{"report", `{"format":"xml"}`, "format"},
		{"report", `{"lang":"fr"}`, "lang"},
		{"note_history", `{"limit":0}`, "limit"},
		{"note_history", `{"limit":101}`, "limit"},
		{"note_history", `{"items":"#1"}`, "items"},
		{"ask_pick", `{"pane":"T2"}`, "n"},
		{"ask_pick", `{"pane":"T2","n":1,"text":"x"}`, "n"},
		{"ask_pick", `{"pane":"T2","n":0}`, "n"},
		{"ask_pick", `{"pane":"T2","text":""}`, "text"},
		{"send_text", `{"pane":"T1"}`, "text"},
		{"send_text", `{"pane":"T1","text":"a","file":"/x"}`, "text"},
		{"send_text", `{"pane":"T1","text":""}`, "text"},
		{"send_escape", `{"confirm":true}`, "pane"},
		{"pane_start", `{"pane":"B1","agent":"vim","cwd":"/x","title":"t"}`, "agent"},
		{"pane_start", `{"pane":"B1","cwd":"/x","title":"t"}`, "agent"},
		{"pane_start", `{"pane":"B1","agent":"omp","cwd":"/x"}`, "title"},
		{"pane_start", `{"pane":"B1","agent":"omp","cwd":"/x","title":"` + long + `"}`, "title"},
		{"pane_start", `{"pane":"B1","agent":"omp","cwd":"/x","title":"a\u0007b"}`, "title"},
		{"pane_start", `{"pane":"B1","agent":"omp","title":"t"}`, "cwd"},
		{"note_set", `{"pane":"T1"}`, "set"},
		{"note_set", `{"pane":"T1","set":null}`, "set"},
		{"note_log", `{"pane":"T1"}`, "text"},
		{"dispatch", `{"agent":"omp","task":"x","profile":"fix"}`, "pane"},
		{"dispatch", `{"pane":"T1","agent":"omp","profile":"fix"}`, "task"},
		{"dispatch", `{"pane":"T1","agent":"omp","task":"x"}`, "profile"},
		{"dispatch", `{"pane":"T1","agent":"omp","task":"x","profile":"nope"}`, "profile"},
		{"dispatch", `{"pane":"T1","agent":"bash","task":"x","profile":"fix"}`, "agent"},
		{"dispatch", `{"pane":"T1","agent":"omp","task":"x","profile":"fix","extra_rules":"a"}`, "extra_rules"},
	}
	for _, c := range cases {
		f := toolstest.New(t)
		_, err := call(t, f, c.tool, c.args)
		d := wantCode(t, err, apperr.InvalidArgs)
		if d["field"] != c.field {
			t.Errorf("%s %s: details.field = %v, want %s", c.tool, c.args, d["field"], c.field)
		}
		if f.Ops.Called() {
			t.Errorf("%s %s: 引数不正なのに操作が呼ばれました", c.tool, c.args)
		}
	}
}

func TestPaneNotFound(t *testing.T) {
	f := toolstest.New(t)
	args := map[string]string{
		"ask_pick": `{"pane":"Z9","n":1}`, "send_text": `{"pane":"Z9","text":"x"}`, "note_log": `{"pane":"Z9","text":"x"}`,
	}
	for _, name := range []string{"pane_tail", "ask_card", "report_read", "note_get", "ask_pick", "send_text", "pane_close", "note_log"} {
		a, ok := args[name]
		if !ok {
			a = `{"pane":"Z9"}`
		}
		_, err := call(t, f, name, a)
		wantCode(t, err, apperr.PaneNotFound)
	}
	if f.Ops.Called() {
		t.Error("解決できないペインに操作が呼ばれました")
	}
}

// 位置は大文字小文字を区別せず、tmux ID でも指せる。
func TestPaneResolution(t *testing.T) {
	f := toolstest.New(t)
	for _, arg := range []string{"t1", "T1", "%1"} {
		m := mustCall(t, f, "pane_tail", `{"pane":"`+arg+`"}`)
		if m["pane"] != "T1" {
			t.Errorf("pane=%s → %v", arg, m["pane"])
		}
	}
}
