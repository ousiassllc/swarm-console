package tooltests_test

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// NFR-PERF-3（docs/requirements/non-functional.md）の ask_card の上限（バイト）。
const maxAskCardBytes = 2 * 1024

// 選択肢が数個の現実的な質問カードでも、ask_card は 2 KB 以内に収まる。
func TestAskCardSizeLimit(t *testing.T) {
	f := toolstest.New(t)
	f.Registry.Cards = map[string]*agent.Card{"%2": {
		Question: "(2) の Go での範囲をどうしますか？ TS 版の仕様どおりに実装するか、ライブラリ関数として拡張するかを選んでください。",
		Cursor:   1, AllowsCustom: true,
		Options: []agent.Option{
			{N: 1, Label: "TS 仕様どおりに実装する（推奨）", Recommended: true},
			{N: 2, Label: "ライブラリ関数として拡張する"},
			{N: 3, Label: "Go では対象外にして仕様書に明記する"},
			{N: 4, Label: "あとで決める"},
		},
	}}
	text, err := call(t, f, "ask_card", `{"pane":"T2"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > maxAskCardBytes {
		t.Errorf("ask_card = %d バイト, 上限 %d", len(text), maxAskCardBytes)
	}
	t.Logf("ask_card = %d バイト", len(text))
}
