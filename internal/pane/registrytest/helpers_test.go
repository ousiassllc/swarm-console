// Package registrytest は pane.SnapshotRegistry をフェイクの Tmux・Host・Adapter・NoteStore で確かめるテスト。
package registrytest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// newTestRegistry は Env のフェイクで Registry を作る。
func newTestRegistry(t *testing.T, env *panetest.Env) *pane.SnapshotRegistry {
	t.Helper()
	r, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: env.FS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustRefresh(t *testing.T, r *pane.SnapshotRegistry) *pane.Snapshot {
	t.Helper()
	s, err := r.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func byID(t *testing.T, s *pane.Snapshot, id string) pane.PaneState {
	t.Helper()
	p, ok := s.Find(id)
	if !ok {
		t.Fatalf("ペイン %s がスナップショットに無い: %+v", id, s.Panes)
	}
	return *p
}

func warningCodes(s *pane.Snapshot) []string {
	var out []string
	for _, w := range s.Warnings {
		out = append(out, w.Code)
	}
	return out
}

// twoRows は 上段 2・下段 1 の 3 ペイン（%0 %1 / %2）を登録する。画面はすべて入力待ちの omp。
func twoRows(env *panetest.Env) []tmux.PaneInfo {
	ps := []tmux.PaneInfo{
		env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"}),
		env.Pane("%1", 101, 0, 50, panetest.ScreenIdle("omp"), []string{"omp"}),
		env.Pane("%2", 102, 20, 0, panetest.ScreenIdle("omp"), []string{"omp"}),
	}
	env.Tmux.SetPanes(ps...)
	return ps
}

func ref(id, pos string) pane.Ref { return pane.Ref{ID: id, Pos: pos} }
