package panetest

import (
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// ProbeAdapter は FakeAdapter に agent.WorkProbe を足したもの。エージェントのログの作業の行を、pid ごとに時刻で与える（Issue #113）。
type ProbeAdapter struct {
	FakeAdapter
	mu   sync.Mutex
	work map[int][]time.Time
}

var (
	_ agent.Adapter   = (*ProbeAdapter)(nil)
	_ agent.WorkProbe = (*ProbeAdapter)(nil)
)

// NewProbeAdapter は作業の行がまだ無い ProbeAdapter を作る。
func NewProbeAdapter(name string) *ProbeAdapter {
	return &ProbeAdapter{FakeAdapter: FakeAdapter{name}, work: map[int][]time.Time{}}
}

// AddWork は pid のログに、at の時刻の作業の行が書かれたことにする。
func (a *ProbeAdapter) AddWork(pid int, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.work[pid] = append(a.work[pid], at)
}

// LastWork は since 以降の作業の行のうち最後の時刻。
func (a *ProbeAdapter) LastWork(pid int, since time.Time) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var last time.Time
	for _, at := range a.work[pid] {
		if !at.Before(since) && at.After(last) {
			last = at
		}
	}
	return last, !last.IsZero()
}
