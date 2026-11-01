package ops

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

// lockMap はペインごとの排他。同じペインは 1 つずつ、別のペインは並行に進める。再入はできない。
type lockMap struct {
	mu      sync.Mutex
	entries map[string]*paneLock
}

type paneLock struct {
	ch   chan struct{} // 容量 1。入れた側が保持者
	refs int           // 保持者と待ち手の数（0 になったら map から消す）
}

func newLockMap() *lockMap { return &lockMap{entries: map[string]*paneLock{}} }

func (m *lockMap) enter(id string) *paneLock {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.entries[id]
	if !ok {
		l = &paneLock{ch: make(chan struct{}, 1)}
		m.entries[id] = l
	}
	l.refs++
	return l
}

func (m *lockMap) leave(id string, l *paneLock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.refs--; l.refs == 0 {
		delete(m.entries, id)
	}
}

// lock は id の排他を取る。timeout を超えたら apperr.BusyTimeout、ctx が終わったら ctx.Err()。
func (m *lockMap) lock(ctx context.Context, clk clock.Clock, id string, timeout time.Duration) (func(), error) {
	l := m.enter(id)
	release := func() {
		<-l.ch
		m.leave(id, l)
	}
	select {
	case l.ch <- struct{}{}:
		return sync.OnceFunc(release), nil
	default:
	}
	select {
	case l.ch <- struct{}{}:
		return sync.OnceFunc(release), nil
	case <-ctx.Done():
		m.leave(id, l)
		return nil, ctx.Err()
	case <-clk.After(timeout):
		m.leave(id, l)
		return nil, apperr.New(apperr.BusyTimeout, "pane", id)
	}
}
