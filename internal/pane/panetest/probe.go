package panetest

import (
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// ProbeAdapter は FakeAdapter に agent.WorkProbe・agent.TurnProbe を足したもの。エージェントのログの作業の行（Issue #113）と、
// 会話の記録の最後のやり取り（Issue #118）を、pid ごとに与える。
type ProbeAdapter struct {
	FakeAdapter
	mu   sync.Mutex
	work map[int][]time.Time
	turn map[int]turnState
}

type turnState struct {
	at     time.Time
	inTurn bool
}

var (
	_ agent.Adapter   = (*ProbeAdapter)(nil)
	_ agent.WorkProbe = (*ProbeAdapter)(nil)
	_ agent.TurnProbe = (*ProbeAdapter)(nil)
)

// NewProbeAdapter は作業の行がまだ無い ProbeAdapter を作る。
func NewProbeAdapter(name string) *ProbeAdapter {
	return &ProbeAdapter{FakeAdapter: FakeAdapter{name}, work: map[int][]time.Time{}, turn: map[int]turnState{}}
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

// SetTurn は pid の会話の記録の最後のやり取りが at で、番の途中（ツールの実行中を含む）なら inTurn、番が終わっていれば !inTurn とする。
func (a *ProbeAdapter) SetTurn(pid int, at time.Time, inTurn bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turn[pid] = turnState{at: at, inTurn: inTurn}
}

// Turn は since 以降の最後のやり取りと、番の途中か。SetTurn していない・since より前なら ok=false。
func (a *ProbeAdapter) Turn(pid int, since time.Time) (time.Time, bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.turn[pid]
	if !ok || t.at.Before(since) {
		return time.Time{}, false, false
	}
	return t.at, t.inTurn, true
}
