package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// cursor_mismatch: カーソルが動かない画面では、カーソル合わせの再試行のあとに中止し、Enter を送らない。
func TestAskPickCursorMismatchSendsNoEnter(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id, keys := h.NewFrozenAsk()
	h.WaitState(id, "asking")
	res := h.Ops().Call("ask_pick", map[string]any{"pane": id, "n": 1})
	if code := res.Code(t); code != "cursor_mismatch" {
		t.Fatalf("ask_pick = %s", res.Text)
	}
	h.Eventually("↑ のキーが届く", func() bool {
		b, _ := os.ReadFile(keys) //nolint:gosec // harness が作った記録ファイル
		return strings.Contains(string(b), "\x1b")
	})
	// 以降に何も届かないことは「届いたキーの全部に Enter が無い」で確かめる（Enter は CR）。
	b, err := os.ReadFile(keys) //nolint:gosec // harness が作った記録ファイル
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(b), "\r\n") {
		t.Errorf("Enter が送られている: %q", b)
	}
	if p, _ := h.Pane(id); p.State != "asking" {
		t.Errorf("state = %s", p.State)
	}
}
