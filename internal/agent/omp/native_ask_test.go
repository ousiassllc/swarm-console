package omp

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/omp/transcript"
)

// 複数選択・recommended が 2 番目のカードも、普通の画面と同じに読める（チェックボックスの記号・カーソルの位置）。
func TestNativeCardForMultiAndRecommendedSecond(t *testing.T) {
	a := fullAdapter(t, &adapter{})
	multi := askCard(transcript.Question{Question: "Toppings", Options: []string{"Cheese", "Ham"}, Multi: true})
	s := agent.NewScreen(append(multi, "", "⟦status⟧"))
	if _, ok := a.liveCard(agent.TrimScreen(s.Lines)); !ok {
		t.Fatal("質問カードの枠と読めない")
	}
	c, ok := a.ParseCard(s)
	if !ok || c.Cursor != 1 || len(c.Options) != 2 || !c.AllowsCustom {
		t.Fatalf("multi: %+v %v", c, ok)
	}
	if keys := a.ToggleKeys(s, 1); len(keys) != 1 || keys[0].Name != "Space" {
		t.Errorf("ToggleKeys = %v, want Space（チェック前の複数選択）", keys)
	}
	second := askCard(transcript.Question{Question: "Pick", Options: []string{"A", "B", "C"}, Recommended: 1, HasRecommended: true})
	c, ok = a.ParseCard(agent.NewScreen(append(second, "")))
	if !ok || c.Cursor != 2 || !c.Options[1].Recommended || c.Options[1].Label != "B" {
		t.Fatalf("recommended 2 番目: %+v %v", c, ok)
	}
}
