package askformtest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// multiScreen は ask-v19-multi-1.txt の選択肢の行（Cheese・Ham・Olives）のカーソルとチェックだけを書き換えた画面。
// 行の形（記号・字下げ・枠）は実画面のまま。
func multiScreen(t *testing.T, cursor int, checked [3]bool) string {
	t.Helper()
	lines := agent.SampleLines(agenttest.Text(t, "omp", "ask-v19-multi-1.txt"))
	row := 0
	for i, l := range lines {
		for _, label := range []string{"Cheese", "Ham", "Olives"} {
			if !strings.Contains(l, " "+label) || !strings.HasPrefix(l, "│") {
				continue
			}
			box := "\uf096"
			if checked[row] {
				box = "\uf14a"
			}
			mark := " "
			if row+1 == cursor {
				mark = "\uf054"
			}
			lines[i] = "│ " + mark + " " + box + " " + label + strings.Repeat(" ", max(len([]rune(l))-len([]rune("│ "+mark+" "+box+" "+label))-1, 0)) + "│"
			row++
		}
	}
	return strings.Join(lines, "\n")
}

// 複数選択の質問は、n 番にチェックが付いていなければ Space で付けてから Enter・Submit の Enter を送る。
// すでに付いていれば Space は送らない（外してしまわない）。
func TestPickAskOmpV19MultiChecksOption(t *testing.T) {
	for _, tc := range []struct {
		name    string
		checked [3]bool // 最初のチェック
		n       int
		want    []string
	}{
		{"1 番", [3]bool{}, 1, []string{"keys %0 Space", "keys %0 Enter", "keys %0 Enter"}},
		{"3 番", [3]bool{}, 3, []string{"keys %0 Down Down", "keys %0 Space", "keys %0 Enter", "keys %0 Enter"}},
		{"チェック済みの 1 番", [3]bool{true}, 1, []string{"keys %0 Enter", "keys %0 Enter"}},
		{"ほかがチェック済みの 2 番", [3]bool{true}, 2, []string{"keys %0 Down", "keys %0 Space", "keys %0 Enter", "keys %0 Enter"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.env.Adapters["omp"] = agenttest.New(t, "omp")
			cursor, checked, phase, enters := 1, tc.checked, "ask", 0
			show := func() {
				switch phase {
				case "submit":
					f.env.Tmux.SetScreen("%0", strings.Join(agent.SampleLines(agenttest.Text(t, "omp", "ask-v19-multi-submit.txt")), "\n"))
				case "done":
					f.env.Tmux.SetScreen("%0", strings.Join(agent.SampleLines(agenttest.Text(t, "omp", "idle-after-ask-v19.txt")), "\n"))
				default:
					f.env.Tmux.SetScreen("%0", multiScreen(t, cursor, checked))
				}
			}
			ref := f.single(multiScreen(t, cursor, checked), []string{"omp"})
			f.env.Tmux.OnOp(func(o panetest.Op) {
				for _, k := range o.Keys {
					switch {
					case k == "Down" && phase == "ask":
						cursor = min(cursor+1, 4)
					case k == "Space" && phase == "ask" && cursor <= 3:
						checked[cursor-1] = !checked[cursor-1]
					case k == "Enter" && phase == "ask":
						phase, enters = "submit", enters+1
					case k == "Enter" && phase == "submit":
						phase, enters = "done", enters+1
					}
				}
				show()
			})
			res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, tc.n, "") })
			if err != nil {
				t.Fatal(err)
			}
			if !res.OK || !res.CardClosed || !checked[tc.n-1] || enters != 2 {
				t.Fatalf("res = %+v, checked = %v, Enter = %d 回", res, checked, enters)
			}
			if got := opStrings(f); !slices.Equal(got, tc.want) {
				t.Fatalf("ops = %v, want %v", got, tc.want)
			}
		})
	}
}
