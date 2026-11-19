// Package askformtest は omp v19 の質問フォーム（ask_pick が Submit の Enter まで送る。Issue #111）を確かめる。
package askformtest

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/omp"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

type fixture struct {
	env  *panetest.Env
	op   *ops.Operator
	logs *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := panetest.NewEnv()
	logs := &bytes.Buffer{}
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: env.FS,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{env: env, op: ops.New(reg), logs: logs}
}

// single は 1 ペイン（%0・T1）だけを登録する。
func (f *fixture) single(screen string, argv []string) pane.Ref {
	f.env.Tmux.SetPanes(f.env.Pane("%0", 100, 0, 0, screen, argv))
	return pane.Ref{ID: "%0", Pos: "T1"}
}

// run は fn を別 goroutine で走らせ、fn が時計（After）で待つたびに Fake の時計を進める。fn が終わるまで繰り返す。
func run[T any](t *testing.T, clk *clock.Fake, fn func() (T, error)) (T, error) {
	t.Helper()
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case r := <-ch:
			return r.v, r.err
		default:
		}
		if clk.Waiters() > 0 {
			clk.Advance(100 * time.Millisecond)
		} else {
			runtime.Gosched()
		}
		if time.Now().After(deadline) {
			t.Fatal("操作が終わりません")
		}
	}
}

func opStrings(f *fixture) []string { return f.env.Tmux.OpStrings() }

// ompV19Ask は omp v19 の質問フォーム（testdata/screens/omp/ask-v19-*.txt。Issue #111）を再現する擬似端末。
// 選択肢の行で Enter を送ると、回答は確定せず、カーソルが Submit の行に移る（質問が 1 行にたたまれる）。
// Submit の行でもう 1 回 Enter を送ると確定し、入力待ちに戻る。画面が変わるのは latency（Fake の時計）のあと。
type ompV19Ask struct {
	t       *testing.T
	f       *fixture
	latency time.Duration

	mu      sync.Mutex
	cursor  int    // 1・2 は選択肢、3 は Other
	phase   string // ask | submit | done
	typed   bool   // Other の行に貼り付けた
	enters  int    // 送られた Enter の数
	pending bool
	showAt  time.Time
}

func (f *fixture) ompV19Ask(t *testing.T, cursor int, latency time.Duration) (pane.Ref, *ompV19Ask) {
	t.Helper()
	f.env.Adapters["omp"] = agenttest.New(t, "omp")
	a := &ompV19Ask{t: t, f: f, latency: latency, cursor: cursor, phase: "ask"}
	ref := f.single(a.screen(), []string{"omp"})
	f.env.Tmux.OnOp(a.onOp)
	f.env.Tmux.OnCapture(a.onCapture)
	return ref, a
}

func (a *ompV19Ask) screen() string {
	file := "ask-v19-1.txt"
	switch {
	case a.phase == "submit":
		file = "ask-v19-submit.txt"
	case a.phase == "done":
		file = "idle-after-ask-v19.txt"
	case a.cursor == 2:
		file = "ask-v19-2.txt"
	case a.cursor == 3:
		file = "ask-v19-other.txt"
	}
	return strings.Join(agent.SampleLines(agenttest.Text(a.t, "omp", file)), "\n")
}

func (a *ompV19Ask) onOp(o panetest.Op) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch o.Kind {
	case "paste":
		a.typed = a.typed || a.cursor == 3
	case "keys":
		for _, k := range o.Keys {
			a.key(k)
		}
	}
	a.pending, a.showAt = true, a.f.env.Clock.Now().Add(a.latency)
}

func (a *ompV19Ask) key(k string) {
	switch {
	case k == "Down" && a.phase == "ask":
		a.cursor = min(a.cursor+1, 3)
	case k == "Up" && a.phase == "ask":
		a.cursor = max(a.cursor-1, 1)
	case k == "Enter":
		a.enters++
		switch {
		case a.phase == "submit":
			a.phase = "done"
		case a.phase == "ask" && (a.cursor < 3 || a.typed): // 何も入れていない Other の行の Enter は何も起きない
			a.phase = "submit"
		}
	}
}

func (a *ompV19Ask) onCapture() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending && !a.f.env.Clock.Now().Before(a.showAt) {
		a.f.env.Tmux.SetScreen("%0", a.screen())
		a.pending = false
	}
}

func (a *ompV19Ask) state() (cursor, enters int, phase string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cursor, a.enters, a.phase
}

// omp v19 では、選んだ Enter のあとカーソルが Submit の行に移り、もう 1 回 Enter を送るまで回答は確定しない。
// ask_pick はその Enter まで送って、カードが閉じたところまで進める。Submit の Enter は 1 回だけ（画面の反映が遅れても重ねない）。
func TestPickAskOmpV19SendsSubmit(t *testing.T) {
	cases := []struct {
		name   string
		cursor int
		n      int
		text   string
		label  string
		want   []string
	}{
		{"1 番のまま", 1, 1, "", sdkRecommendLabel, []string{"keys %0 Enter", "keys %0 Enter"}},
		{"1 番から 2 番", 1, 2, "", "Attach artifacts to GitHub Releases manually", []string{"keys %0 Down", "keys %0 Enter", "keys %0 Enter"}},
		{"Other の行から 1 番", 3, 1, "", sdkRecommendLabel, []string{"keys %0 Up Up", "keys %0 Enter", "keys %0 Enter"}},
		{"Other（自由入力）", 1, 0, "自由な答え", "", []string{"keys %0 Down Down", `paste %0 "自由な答え"`, "keys %0 Enter", "keys %0 Enter"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, latency := range []time.Duration{0, 400 * time.Millisecond} {
				f := newFixture(t)
				ref, form := f.ompV19Ask(t, tc.cursor, latency)
				res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, tc.n, tc.text) })
				if err != nil {
					t.Fatal(err)
				}
				if !res.OK || !res.CardClosed || res.State != agent.StateIdle {
					t.Fatalf("latency=%v: res = %+v", latency, res)
				}
				if tc.text == "" && (res.Picked == nil || res.Picked.N != tc.n || res.Picked.Label != tc.label) {
					t.Fatalf("picked = %+v", res.Picked)
				}
				if got := opStrings(f); !slices.Equal(got, tc.want) {
					t.Fatalf("latency=%v: ops = %v, want %v", latency, got, tc.want)
				}
				if _, enters, phase := form.state(); enters != 2 || phase != "done" {
					t.Fatalf("Enter = %d 回, phase = %s", enters, phase)
				}
			}
		})
	}
}

const sdkRecommendLabel = "Recommended: keep CI artifacts only and file a separate issue for registry publish, and mention it as unmet in the report so that humans can follow up later on"

// 質問が複数のフォームで、1 つ目（複数選択）を選ぶ Space・Enter のあとに次の質問が開いたら、閉じていない（card_closed=false）まま、すぐ返す。
// 次の質問は ask_card で読み直して選ぶ。
func TestPickAskOmpV19FormNextQuestion(t *testing.T) {
	f := newFixture(t)
	f.env.Adapters["omp"] = agenttest.New(t, "omp")
	screen := func(file string) string { return strings.Join(agent.SampleLines(agenttest.Text(t, "omp", file)), "\n") }
	ref := f.single(screen("ask-v19-form-1.txt"), []string{"omp"})
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "keys" && len(o.Keys) == 1 && o.Keys[0] == "Space":
			f.env.Tmux.SetScreen("%0", screen("ask-v19-form-1-checked.txt"))
		case o.Kind == "keys" && len(o.Keys) == 1 && o.Keys[0] == "Enter":
			f.env.Tmux.SetScreen("%0", screen("ask-v19-form-2.txt"))
		}
	})
	res, err := run(t, f.env.Clock, func() (pane.PickResult, error) { return f.op.PickAsk(context.Background(), ref, 1, "") })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.CardClosed || res.State != agent.StateAsking || res.Picked == nil || res.Picked.Label != "Cheese" {
		t.Fatalf("res = %+v", res)
	}
	if got := opStrings(f); !slices.Equal(got, []string{"keys %0 Space", "keys %0 Enter"}) {
		t.Fatalf("ops = %v", got)
	}
}
