package webhook

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Issue #118: 台帳の版は 2 のまま。`watch`（受理を確かめられなかった指示の見張り）は省略できる任意のキーなので、
// 持たない台帳（#118 より前の版 2）もそのまま読め、読んだ見張りは Unaccepted に引き継がれる。形の崩れた行は捨てる。
func TestLedgerWatchIsOptionalAndHandedToUnaccepted(t *testing.T) {
	enter := time.Date(2026, 11, 20, 7, 8, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		content string
		want    map[string]pane.Watch
	}{
		"watch が無い（#118 より前の版 2）": {
			content: `{"version":2,"panes":{},"outbox":{}}`,
			want:    map[string]pane.Watch{},
		},
		"watch がある": {
			content: `{"version":2,"panes":{},"watch":{"%3":{"enter":"2026-11-20T07:08:00Z","retained":4,"pid":321},"%4":{"enter":"2026-11-20T07:08:00Z"}},"outbox":{}}`,
			want: map[string]pane.Watch{
				"%3": {Enter: enter, Retained: 4, PID: 321},
				"%4": {Enter: enter},
			},
		},
		"Enter の時刻が無い行は捨てる": {
			content: `{"version":2,"panes":{},"watch":{"%3":{"pid":321},"%4":{"enter":"2026-11-20T07:08:00Z"}},"outbox":{}}`,
			want:    map[string]pane.Watch{"%4": {Enter: enter}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if err := os.WriteFile(h.state, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			un := pane.NewUnaccepted()
			h.un = un
			h.start()
			if strings.Contains(h.logs.String(), "webhook の台帳を読めませんでした") {
				t.Fatalf("読めるはずの台帳で警告:\n%s", h.logs.String())
			}
			got := un.Export()
			if len(got) != len(tc.want) {
				t.Fatalf("見張り = %+v, want %+v", got, tc.want)
			}
			for id, w := range tc.want {
				g, ok := got[id]
				if !ok || !g.Enter.Equal(w.Enter) || g.Retained != w.Retained || g.PID != w.PID {
					t.Errorf("%s: %+v, want %+v", id, g, w)
				}
			}
		})
	}
}

// 台帳に書いた見張りは、Unaccepted の変化に合わせて増減し（受理されて消えれば台帳からも消える）、tmux の再起動で捨てる。
func TestLedgerWatchFollowsUnaccepted(t *testing.T) {
	h := newHarness(t)
	un := pane.NewUnaccepted()
	h.un = un
	h.srvStart = "100"
	h.start()
	enter := time.Date(2026, 11, 20, 7, 8, 0, 0, time.UTC)
	un.Mark("%3", enter)
	h.observe(agent.StateIdle)
	if w := readWatch(t, h.state); len(w) != 1 || !w["%3"].Enter.Equal(enter) {
		t.Fatalf("台帳の watch = %+v", w)
	}
	un.Clear("%3") // 受理を確かめられた
	h.observe(agent.StateIdle)
	if w := readWatch(t, h.state); len(w) != 0 {
		t.Fatalf("受理されたのに台帳に残っている: %+v", w)
	}

	un.Mark("%3", enter)
	h.observe(agent.StateIdle)
	h.srvStart = "200" // tmux サーバーの再起動
	h.observe(agent.StateIdle)
	if w := readWatch(t, h.state); len(w) != 0 {
		t.Fatalf("tmux の再起動のあとも見張りが残っている: %+v", w)
	}
	if _, ok := un.Since("%3"); ok {
		t.Fatal("tmux の再起動のあとも Unaccepted に残っている")
	}
}

func readWatch(t *testing.T, path string) map[string]struct {
	Enter time.Time `json:"enter"`
} {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // テストの一時ディレクトリの台帳
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Watch map[string]struct {
			Enter time.Time `json:"enter"`
		} `json:"watch"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f.Watch
}
