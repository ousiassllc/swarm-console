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

	reaction      map[int]agent.ReactionReading
	reactionCalls []ReactionCall
	reactionAfter map[int]reactionStep
}

type reactionStep struct {
	calls int
	r     agent.ReactionReading
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

// ReactionCall は ProbeAdapter.Reaction が呼ばれた記録。
type ReactionCall struct {
	PID   int
	Since time.Time
	Text  string
}

var _ agent.ReactionProbe = (*ProbeAdapter)(nil)

// SetReaction は pid の会話の記録が、いまこう読めることにする（Issue #120）。
func (a *ProbeAdapter) SetReaction(pid int, r agent.ReactionReading) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reaction == nil {
		a.reaction = map[int]agent.ReactionReading{}
	}
	a.reaction[pid] = r
}

// SetReactionAfter は pid の読み取りが calls 回目に達したら、r を返すようにする（記録があとから進むことの再現）。
func (a *ProbeAdapter) SetReactionAfter(pid, calls int, r agent.ReactionReading) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reactionAfter == nil {
		a.reactionAfter = map[int]reactionStep{}
	}
	a.reactionAfter[pid] = reactionStep{calls: calls, r: r}
}

// Reaction は SetReaction した読み取り。していなければ、記録に現れない（unverified・not_found）。呼び出しは ReactionCalls に残る。
func (a *ProbeAdapter) Reaction(pid int, since time.Time, text string) agent.ReactionReading {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reactionCalls = append(a.reactionCalls, ReactionCall{PID: pid, Since: since, Text: text})
	if st, ok := a.reactionAfter[pid]; ok && len(a.reactionCalls) >= st.calls {
		return st.r
	}
	if r, ok := a.reaction[pid]; ok {
		return r
	}
	return agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisNotFound}
}

// ReactionCalls は Reaction が呼ばれた記録。
func (a *ProbeAdapter) ReactionCalls() []ReactionCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ReactionCall(nil), a.reactionCalls...)
}
