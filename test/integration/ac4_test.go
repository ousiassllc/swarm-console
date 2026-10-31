package integration

import (
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// AC-4: 同じ dispatch の引数を 2 つのクライアントから送ると、ペインに入る指示文が同じ。
// 同じペインへ（pane_close してから）2 回送ったとき 1 文字違わず、別のペインへ送ったときは最終報告のパス（位置）だけが違う。
func TestAC4DispatchSameTextFromTwoClients(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	pasted := func(id string, n int) string {
		ev := h.WaitEvents(id, "貼り付けが届く", func(ev []fakeEvent) bool {
			c := 0
			for _, e := range ev {
				if e.Kind == "PASTE" {
					c++
				}
			}
			return c >= n
		})
		c := 0
		for _, e := range ev {
			if e.Kind == "PASTE" {
				if c++; c == n {
					return e.Text
				}
			}
		}
		return ""
	}
	mk := func(id string) map[string]any {
		a := dispatchArgs(id)
		a["cwd"], a["title"] = h.Work, "funid #982"
		return a
	}

	// 別のペイン（クライアント A → ペイン 1、クライアント B → ペイン 2）。
	p1, p2 := h.NewShell(), h.NewShell()
	resA := h.Ops().Map("dispatch", mk(p1))
	resB := h.OpsB().Map("dispatch", mk(p2))
	pos1, pos2 := h.PosOf(p1), h.PosOf(p2)
	textA, textB := pasted(p1, 1), pasted(p2, 1)
	if resA["sent"] != textA || resB["sent"] != textB {
		t.Errorf("sent と貼られた本文が違う")
	}
	norm := func(s, pos string) string { return strings.ReplaceAll(s, pane.ReportPath(h.Reports, pos), "<REPORT>") }
	if norm(textA, pos1) != norm(textB, pos2) || !strings.Contains(textA, pane.ReportPath(h.Reports, pos1)) {
		t.Errorf("別のペインで指示文が違う:\n%s\n---\n%s", textA, textB)
	}

	// 同じペインへ、A のあとに B（間に pane_close）。
	h.WaitState(p1, "working")
	h.Ops().Must("send_escape", map[string]any{"pane": p1, "confirm": true})
	if res := h.Ops().Map("pane_close", map[string]any{"pane": p1}); res["ok"] != true {
		t.Fatalf("pane_close = %v", res)
	}
	h.WaitState(p1, "shell")
	resB2 := h.OpsB().Map("dispatch", mk(p1))
	if resB2["ok"] != true || resB2["sent"] != resA["sent"] {
		t.Errorf("同じペインへの 2 回目:\n%v\n--- 1 回目の sent\n%v", resB2["sent"], resA["sent"])
	}
	if got := pasted(p1, 2); got != textA {
		t.Errorf("同じペインに 2 回貼られた本文が 1 文字違う:\n%s\n---\n%s", textA, got)
	}
}
