package integration

import (
	"testing"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// ask_pick: omp は ↑/↓、claude・codex は数字。キー列が実 tmux 経由で擬似エージェントに届き、選ばれた番号が --log に残る。
func TestAskPick(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	tests := []struct {
		agent, scenario string
		n               int
		label           string
	}{
		{"omp", "ask", 1, "Apple"},
		{"omp", "ask", 2, "Banana"},
		{"omp", "ask", 3, "Cherry"},
		{"claude", "ask", 2, "Yes, and don’t ask again for touch commands in /tmp/sc-probe/wd"},
		{"claude", "ask-custom", 3, "Cherry"},
	}
	for _, tt := range tests {
		t.Run(tt.agent+"-"+tt.scenario+"-"+tt.label[:1], func(t *testing.T) {
			id := h.NewAgent(tt.agent, tt.scenario)
			h.WaitState(id, "asking")
			res := h.Ops().Map("ask_pick", map[string]any{"pane": id, "n": tt.n})
			picked, _ := res["picked"].(map[string]any)
			if res["ok"] != true || res["state"] != "working" || res["card_closed"] != true ||
				picked["n"] != float64(tt.n) || picked["label"] != tt.label {
				t.Errorf("ask_pick = %v", res)
			}
			ev := h.WaitEvents(id, "PICK が記録される", func(ev []fakeEvent) bool { return len(ev) > 0 })
			if len(ev) != 1 || ev[0].Kind != "PICK" || ev[0].N != tt.n {
				t.Errorf("--log = %+v, want PICK %d だけ", ev, tt.n)
			}
		})
	}
}

// ask_pick の text（omp）: 「その他」へカーソルを動かして開き、自由入力へ本文を貼って Enter。選択肢の PICK は記録されない。
// 最初の貼り付けは「その他」を開くための Enter と一緒に出るので、貼り付けは 2 回記録される（アダプタの submitCustom の仕様）。
func TestAskPickCustomText(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewAgent("omp", "ask")
	h.WaitState(id, "asking")
	const text = "自由入力です\n2行目"
	res := h.Ops().Map("ask_pick", map[string]any{"pane": id, "text": text})
	if res["ok"] != true || res["state"] != "working" || res["card_closed"] != true || res["picked"] != nil {
		t.Errorf("ask_pick = %v", res)
	}
	ev := h.WaitEvents(id, "CUSTOM が記録される", func(ev []fakeEvent) bool {
		return len(ev) > 0 && ev[len(ev)-1].Kind == "CUSTOM"
	})
	if last := ev[len(ev)-1]; last.Text != text {
		t.Errorf("CUSTOM = %q, want %q", last.Text, text)
	}
	for _, e := range ev {
		if e.Kind == "PICK" || e.Kind == "SUBMIT" {
			t.Errorf("想定外の出来事: %v", harness.Kinds(ev))
		}
	}
}

// Codex は Up/Down でカーソルを n へ動かしてから Enter で確定する（PICK は 1 回だけ）。数字キーは実画面で押した時点で確定するので使わない。
func TestAskPickCodexArrows(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	for _, n := range []int{1, 3} {
		id := h.NewAgent("codex", "ask")
		h.WaitState(id, "asking")
		res := h.Ops().Map("ask_pick", map[string]any{"pane": id, "n": n})
		if res["ok"] != true || res["state"] != "working" {
			t.Errorf("n=%d: %v", n, res)
		}
		ev := h.WaitEvents(id, "PICK", func(ev []fakeEvent) bool { return len(ev) > 0 })
		if len(ev) != 1 || ev[0].Kind != "PICK" || ev[0].N != n {
			t.Errorf("n=%d: --log = %+v", n, ev)
		}
	}
}

// Claude Code の自由入力（ask-custom）。「その他」へ Down → 貼り付け → Enter。擬似エージェントは、カーソルが
// 「その他」にあるとき貼り付けをその項の本文として受け、Enter で確定する（実画面は未確認。推定のサンプル）。
func TestAskPickClaudeCustomText(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewAgent("claude", "ask-custom")
	h.WaitState(id, "asking")
	res := h.Ops().Map("ask_pick", map[string]any{"pane": id, "text": "自由入力"})
	if res["ok"] != true || res["state"] != "working" {
		t.Errorf("ask_pick = %v", res)
	}
	ev := h.WaitEvents(id, "CUSTOM", func(ev []fakeEvent) bool { return len(ev) > 0 && ev[len(ev)-1].Kind == "CUSTOM" })
	if ev[len(ev)-1].Text != "自由入力" {
		t.Errorf("--log = %+v", ev)
	}
}
