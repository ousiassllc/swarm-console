package pane

import (
	"sync"
	"time"
)

// Unaccepted は、指示（send_text・dispatch）の Enter を送ったのに、受理（working になった）を画面で確かめられなかったペインを覚える（Issue #113）。
// omp は負荷が高いと画面の描き直しが止まり、作業を始めても起動画面のまま idle と読めることがある。
// 操作（ops）が書き、スナップショットの組み立て（Registry）が読んで、そのペインだけエージェントのログを見て作業中かを確かめる。
type Unaccepted struct {
	mu sync.Mutex
	at map[string]time.Time
}

// NewUnaccepted は空の Unaccepted を作る。
func NewUnaccepted() *Unaccepted { return &Unaccepted{at: map[string]time.Time{}} }

// Mark は id のペインに、at に Enter を送ったが受理を確かめられなかったことを覚える。覚えていれば上書きする。
func (u *Unaccepted) Mark(id string, at time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.at[id] = at
}

// Clear は id のペインの記録を消す。
func (u *Unaccepted) Clear(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.at, id)
}

// Since は id のペインが受理を確かめられなかった Enter の時刻。無ければ ok=false。
func (u *Unaccepted) Since(id string) (at time.Time, ok bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	at, ok = u.at[id]
	return at, ok
}
