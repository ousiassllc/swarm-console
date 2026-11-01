package integration

import (
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// panes_status: 段・列の位置名（T1・T2・B1）と layout が、実 tmux の座標から決まる。
func TestPanesStatusPositions(t *testing.T) {
	h := harness.Start(t, harness.Options{Width: 250, Height: 85})
	a := h.P0
	b := strings.TrimSpace(h.TM.Run("split-window", "-h", "-d", "-P", "-F", "#{pane_id}", "-t", a))
	c := strings.TrimSpace(h.TM.Run("split-window", "-v", "-d", "-P", "-F", "#{pane_id}", "-t", a))
	for _, id := range []string{a, b, c} {
		h.WaitState(id, "shell")
	}
	st := h.Status()
	if st.Layout.Rows != 2 || len(st.Layout.Cols) != 2 || st.Layout.Cols[0] != 2 || st.Layout.Cols[1] != 1 {
		t.Errorf("layout = %+v, want rows 2 cols [2 1]", st.Layout)
	}
	want := map[string]string{a: "T1", b: "T2", c: "B1"}
	for _, p := range st.Panes {
		if want[p.ID] != p.Pos {
			t.Errorf("%s の位置 = %s, want %s", p.ID, p.Pos, want[p.ID])
		}
		if p.Agent != nil || p.State != "shell" {
			t.Errorf("%s: agent=%v state=%s", p.ID, p.Agent, p.State)
		}
	}
	// 位置の根拠は tmux の座標（a と b は同じ段、c は下の段）。
	top := func(id string) string {
		return strings.TrimSpace(h.TM.Run("display-message", "-p", "-t", id, "#{pane_top}"))
	}
	if top(a) != top(b) || top(a) == top(c) {
		t.Errorf("座標が想定と違う: a=%s b=%s c=%s", top(a), top(b), top(c))
	}
	// 位置は大文字小文字を区別せず、ID でも指せる。
	for _, arg := range []string{"t2", "T2", b} {
		if got := h.Ops().Map("pane_tail", map[string]any{"pane": arg})["pane"]; got != "T2" {
			t.Errorf("pane=%s → %v, want T2", arg, got)
		}
	}
	if n := len(h.Ops().Map("panes_status", map[string]any{"session": harness.Session})["panes"].([]any)); n != 3 {
		t.Errorf("session 指定のペイン数 = %d", n)
	}
	if code := h.Ops().Call("panes_status", map[string]any{"session": "nope"}).Code(t); code != "invalid_args" {
		t.Errorf("未知のセッション: %s", code)
	}
}

// pane_tail: シェルのペインは飾りを除かず本文、エージェントのペインは飾り除去あり・raw で全部。範囲外の lines は拒否。
func TestPaneTail(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	h.Type(h.P0, "echo hello-tail-marker")
	h.TM.WaitFor(h.P0, func(s string) bool { return strings.Count(s, "hello-tail-marker") >= 2 })
	tail := h.Ops().Map("pane_tail", map[string]any{"pane": h.P0, "lines": 10})
	if text, _ := tail["text"].(string); !strings.Contains(text, "hello-tail-marker") || tail["lines"] != float64(10) {
		t.Errorf("shell の tail = %v", tail)
	}

	id := h.NewAgent("omp", "idle")
	h.WaitState(id, "idle")
	rawText, _ := h.Ops().Map("pane_tail", map[string]any{"pane": id, "lines": 200, "raw": true})["text"].(string)
	cleanText, _ := h.Ops().Map("pane_tail", map[string]any{"pane": id, "lines": 200})["text"].(string)
	if !strings.Contains(rawText, "READY") || len(cleanText) >= len(rawText) {
		t.Errorf("raw=%d 字 clean=%d 字（飾りを除いた方が短いはず）", len(rawText), len(cleanText))
	}
	for _, n := range []any{0, 201, -1, "x"} {
		if code := h.Ops().Call("pane_tail", map[string]any{"pane": id, "lines": n}).Code(t); code != "invalid_args" {
			t.Errorf("lines=%v: %s", n, code)
		}
	}
}

// ask_card: omp・claude（許可確認・質問）・codex のカードを実画面から読む。
func TestAskCard(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	tests := []struct {
		agent, scenario string
		question        string
		options, cursor int
		custom          bool
	}{
		{"omp", "ask", "Pick a fruit", 3, 2, true},
		{"claude", "ask", "Do you want to proceed?", 3, 1, false},
		{"claude", "ask-custom", "Pick a fruit?", 3, 2, true},
		{"codex", "ask", "Would you like to run the following command?", 3, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.agent+"-"+tt.scenario, func(t *testing.T) {
			id := h.NewAgent(tt.agent, tt.scenario)
			h.WaitState(id, "asking")
			card := h.Ops().Map("ask_card", map[string]any{"pane": id})
			opts, _ := card["options"].([]any)
			if card["asking"] != true || card["question"] != tt.question || len(opts) != tt.options ||
				card["cursor"] != float64(tt.cursor) || card["allows_custom"] != tt.custom {
				t.Errorf("card = %v", card)
			}
		})
	}
	id := h.NewAgent("omp", "idle")
	h.WaitState(id, "idle")
	if got := h.Ops().Must("ask_card", map[string]any{"pane": id}).Text; got != `{"asking":false}` {
		t.Errorf("質問のないペイン: %s", got)
	}
}
