package opstest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/omp"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// ompAskLag は実機の omp の質問カード（「どちらが好き？」の A・B と Other。testdata/screens/omp/ask-ab-*.txt）を再現する擬似端末。
// キーはすぐ受け取るが、画面に反映されるのは latency（Fake の時計）のあと。実機でキーを送った直後の capture-pane が
// 動く前のカーソルを返したのを再現する（Issue #20）。カーソルは端で止まる（omp の挙動）。
type ompAskLag struct {
	t       *testing.T
	f       *fixture
	latency time.Duration

	mu      sync.Mutex
	cursor  int       // omp の中のカーソル
	pending bool      // 画面が追いついていない
	showAt  time.Time // 画面に反映される時刻
}

func (f *fixture) ompAskLag(t *testing.T, cursor int, latency time.Duration) (pane.Ref, *ompAskLag) {
	t.Helper()
	f.env.Adapters["omp"] = agenttest.New(t, "omp")
	a := &ompAskLag{t: t, f: f, latency: latency, cursor: cursor}
	ref := f.single(a.screen(), []string{"omp"})
	f.env.Tmux.OnOp(a.onOp)
	f.env.Tmux.OnCapture(a.onCapture)
	return ref, a
}

func (a *ompAskLag) screen() string {
	text := agenttest.Text(a.t, "omp", fmt.Sprintf("ask-ab-%d.txt", a.cursor))
	return strings.Join(agent.SampleLines(text), "\n")
}

func (a *ompAskLag) onOp(o panetest.Op) {
	if o.Kind != "keys" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range o.Keys {
		switch k {
		case "Down":
			a.cursor = min(a.cursor+1, 3)
		case "Up":
			a.cursor = max(a.cursor-1, 1)
		case "Enter":
			a.f.env.Tmux.SetScreen("%0", agenttest.Text(a.t, "omp", "working-after-ask.txt"))
			a.pending = false
			return
		}
	}
	a.pending, a.showAt = true, a.f.env.Clock.Now().Add(a.latency)
}

func (a *ompAskLag) onCapture() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending && !a.f.env.Clock.Now().Before(a.showAt) {
		a.f.env.Tmux.SetScreen("%0", a.screen())
		a.pending = false
	}
}

func (a *ompAskLag) cursorNow() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cursor
}

func TestPickAskOmpCardWithRenderLag(t *testing.T) {
	cases := []struct {
		name   string
		cursor int // 最初のカーソル
		n      int
		text   string
		label  string
		want   []string
	}{
		{"1 番から 2 番（Issue #20）", 1, 2, "", "B", []string{"keys %0 Down", "keys %0 Enter"}},
		{"2 番から 1 番", 2, 1, "", "A", []string{"keys %0 Up", "keys %0 Enter"}},
		{"Other の行から 2 番", 3, 2, "", "B", []string{"keys %0 Up", "keys %0 Enter"}},
		{"1 番のまま 1 番", 1, 1, "", "A", []string{"keys %0 Enter"}},
		{"Other（自由入力）", 1, 0, "自由な答え", "", []string{"keys %0 Down Down", `paste %0 "自由な答え"`, "keys %0 Enter"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ref, card := f.ompAskLag(t, tc.cursor, 300*time.Millisecond)
			res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, tc.n, tc.text) })
			if err != nil {
				t.Fatal(err)
			}
			if !res.OK || !res.CardClosed || res.State != agent.StateWorking {
				t.Fatalf("res = %+v", res)
			}
			if tc.text == "" && (res.Picked == nil || res.Picked.N != tc.n || res.Picked.Label != tc.label) {
				t.Fatalf("picked = %+v", res.Picked)
			}
			if got := opStrings(f); !equalStrings(got, tc.want) {
				t.Fatalf("ops = %v, want %v", got, tc.want)
			}
			want := tc.n
			if tc.text != "" {
				want = 3
			}
			if got := card.cursorNow(); got != want {
				t.Fatalf("Enter を送った時のカーソル = %d, want %d", got, want)
			}
		})
	}
}
