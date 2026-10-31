package pane

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

func TestRefreshWhileInFlightTakesAnotherSnapshot(t *testing.T) {
	env := panetest.NewEnv()
	env.Tmux.SetPanes(env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}))
	r, err := NewRegistry(Deps{Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: env.FS})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	env.Tmux.OnList(func() { <-gate })

	first := make(chan *Snapshot, 1)
	go func() { s, _ := r.Refresh(context.Background()); first <- s }()
	panetest.WaitUntil(t, "1 回目が進行中", func() bool { return env.Tmux.ListCalls() == 1 })
	second := make(chan *Snapshot, 1)
	go func() { s, _ := r.Refresh(context.Background()); second <- s }()
	panetest.WaitUntil(t, "2 回目が待機列に入る", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.queued != nil
	})
	close(gate)
	s1, s2 := <-first, <-second
	if s1 == s2 || env.Tmux.ListCalls() != 2 {
		t.Fatalf("進行中の取得は呼び出し前に始まったもの。もう 1 回取る: same=%v calls=%d", s1 == s2, env.Tmux.ListCalls())
	}
}
