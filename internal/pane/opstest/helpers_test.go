// Package opstest は ops.Operator をフェイクの Tmux・Host・Adapter・NoteStore で確かめるテスト。
package opstest

import (
	"runtime"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

type fixture struct {
	env *panetest.Env
	reg *pane.SnapshotRegistry
	op  *ops.Operator
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := panetest.NewEnv()
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: env.FS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{env: env, reg: reg, op: ops.New(reg)}
}

// single は 1 ペイン（%0・T1）だけを登録する。
func (f *fixture) single(screen string, argv []string) pane.Ref {
	f.env.Tmux.SetPanes(f.env.Pane("%0", 100, 0, 0, screen, argv))
	return pane.Ref{ID: "%0", Pos: "T1"}
}

// becomeAgent は %0 のプロセス木と画面を、エージェントが起動した状態にする。
func (f *fixture) becomeAgent(argv []string, screen string) {
	f.env.Host.SetTree(100, []string{"bash"}, argv)
	f.env.Tmux.SetScreen("%0", screen)
}

// becomeShell は %0 を、エージェントが終了したシェルの状態にする。
func (f *fixture) becomeShell(screen string) {
	f.env.Host.SetTree(100, []string{"bash"})
	f.env.Tmux.SetScreen("%0", screen)
}

// run は fn を別 goroutine で走らせ、fn が時計（After）で待つたびに Fake の時計を進める。
// fn が終わるまで繰り返す。固定の sleep は使わない。
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

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
