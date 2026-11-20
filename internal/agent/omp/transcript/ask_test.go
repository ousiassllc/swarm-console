package transcript

import (
	"strings"
	"testing"
)

func TestParseAskForms(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []Question
	}{
		{"html の単一選択（checked が recommended）",
			`{"html":"<form><fieldset><legend>Pick</legend><label><input type=\"radio\" name=\"c\" value=\"A\"> A</label><label><input type=\"radio\" name=\"c\" value=\"B\" checked> B &amp; C</label></fieldset></form>"}`,
			[]Question{{Question: "Pick", Options: []string{"A", "B & C"}, Recommended: 1, HasRecommended: true}}},
		{"html の複数選択と 2 問目",
			`{"html":"<form><fieldset><legend>Toppings</legend><label><input type=\"checkbox\" name=\"t\" value=\"x\"> Cheese</label><label><input type=\"checkbox\" name=\"t\" value=\"y\"> Ham</label></fieldset><fieldset><legend>Size</legend><select name=\"s\"><option>S</option><option selected>L</option></select></fieldset></form>"}`,
			[]Question{
				{Question: "Toppings", Options: []string{"Cheese", "Ham"}, Multi: true},
				{Question: "Size", Options: []string{"S", "L"}, Recommended: 1, HasRecommended: true},
			}},
		{"questions の配列（文字列とオブジェクトの選択肢）",
			`{"questions":[{"id":"q1","question":"Which?","options":["one",{"label":"two","description":"d"}],"multi":false,"recommended":0}]}`,
			[]Question{{Question: "Which?", Options: []string{"one", "two"}, HasRecommended: true}}},
		{"読めない引数", `{"x":1}`, nil},
		{"壊れた JSON", `not json`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseAsk([]byte(c.args))
			if len(got) != len(c.want) {
				t.Fatalf("質問の数 = %d, want %d: %+v", len(got), len(c.want), got)
			}
			for i := range got {
				g, w := got[i], c.want[i]
				if g.Question != w.Question || g.Multi != w.Multi || g.Recommended != w.Recommended || g.HasRecommended != w.HasRecommended || strings.Join(g.Options, "|") != strings.Join(w.Options, "|") {
					t.Errorf("[%d] got=%+v want=%+v", i, g, w)
				}
			}
		})
	}
}
