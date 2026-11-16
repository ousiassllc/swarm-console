package registrytest

import (
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// fakeObserver は pane.SnapshotObserver。受け取ったスナップショットを順に覚える。
type fakeObserver struct {
	mu    sync.Mutex
	snaps []*pane.Snapshot
}

func (o *fakeObserver) Observe(s *pane.Snapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.snaps = append(o.snaps, s)
}

func (o *fakeObserver) seen() []*pane.Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]*pane.Snapshot(nil), o.snaps...)
}

func newObservedRegistry(t *testing.T, env *panetest.Env, obs pane.SnapshotObserver) *pane.SnapshotRegistry {
	t.Helper()
	r, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters,
		Notes: env.Notes, Clock: env.Clock, FS: env.FS, Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Issue #93: Observer は、スナップショットを作るたびに 1 回、作った順に、Refresh が返すのと同じものを受け取る。
func TestObserverReceivesEachBuiltSnapshotInOrder(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	obs := &fakeObserver{}
	r := newObservedRegistry(t, env, obs)

	first := mustRefresh(t, r)
	if got := obs.seen(); len(got) != 1 || got[0] != first {
		t.Fatalf("1 回目: observer が受けたもの = %d 個（Refresh の結果と同じ 1 個のはず）", len(got))
	}
	env.Clock.Advance(1)
	second := mustRefresh(t, r)
	got := obs.seen()
	if len(got) != 2 || got[0] != first || got[1] != second || first == second {
		t.Fatalf("2 回目: observer が受けたもの = %d 個（順に Refresh の結果と同じはず）", len(got))
	}
}

// tmux の一覧を読めなくても build は（tmux_unavailable の警告つきで）スナップショットを返すので、空のペインで Observer を呼ぶ。
func TestObserverIsCalledWithEmptySnapshotWhenTmuxFails(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	obs := &fakeObserver{}
	r := newObservedRegistry(t, env, obs)
	mustRefresh(t, r)

	env.Tmux.SetListErr(errBoom)
	s := mustRefresh(t, r)
	got := obs.seen()
	if len(got) != 2 || got[1] != s || len(got[1].Panes) != 0 || got[1].ServerStart != "" {
		t.Fatalf("tmux 不通: observer が受けたもの = %+v", got)
	}
	if codes := warningCodes(got[1]); len(codes) != 1 || codes[0] != pane.WarnTmuxUnavailable {
		t.Errorf("警告 = %v", codes)
	}
}

// Observer を渡さなければ（nil）何も起きない。
func TestNilObserverDoesNothing(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newObservedRegistry(t, env, nil)
	if s := mustRefresh(t, r); len(s.Panes) != 3 {
		t.Fatalf("panes = %d", len(s.Panes))
	}
}

// Snapshot.ServerStart は tmux の起動時刻。サーバーが無い（空）ときは空。
func TestSnapshotServerStartFromTmux(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	if got := mustRefresh(t, r).ServerStart; got != "1000" {
		t.Fatalf("ServerStart = %q, want 1000", got)
	}
	env.Tmux.SetServerStart("2000")
	if got := mustRefresh(t, r).ServerStart; got != "2000" {
		t.Fatalf("ServerStart = %q, want 2000", got)
	}
	env.Tmux.SetServerStart("")
	if got := mustRefresh(t, r).ServerStart; got != "" {
		t.Fatalf("ServerStart = %q, want 空", got)
	}
}
