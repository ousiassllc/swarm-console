package pane

import (
	"sync"
	"time"
)

// maxRetained は、件を送ったあとも見続ける（Retain した）ペインの数の上限（Issue #115。decisions.md P-107）。
// 超えたら、Retain したのが最も古いペインから見るのをやめる。ログを読む費用に上限を置くため。
const maxRetained = 16

// Unaccepted は、指示（send_text・dispatch）の Enter を送ったのに、受理（working になった）を画面で確かめられなかったペインを覚える（Issue #113）。
// omp は負荷が高いと画面の描き直しが止まり、作業を始めても起動画面のまま idle と読めることがある。
// 操作（ops）が書き、スナップショットの組み立て（Registry）が読んで、そのペインだけエージェントのログを見て作業中かを確かめる。
//
// 件を送ったあと（webhook が pane.idle_report の回を done にしたあと）は Retain で、10 分の上限を外して見続ける（Issue #115）。
type Unaccepted struct {
	mu  sync.Mutex
	at  map[string]*unacceptedEntry
	seq uint64 // Retain の順（古いほうから捨てる）
}

type unacceptedEntry struct {
	enter    time.Time // 受理を確かめられなかった Enter の時刻
	retained uint64    // 0 でなければ Retain した順。上限（10 分）で捨てない
	// 以下は Registry がログを読んだ結果。Retain したペインは読む間隔をあけるので、次に読むまで使い回す。
	pid     int       // ログを読んだエージェントの pid（0 は未読）
	checked time.Time // ログを読んだ時刻（ゼロは未読）
	last    time.Time // ログの最後の作業の行
	worked  bool      // last が使えるか
}

// logProbe は 1 ペインのログの確認の記録（Registry が読み書きする写し）。
type logProbe struct {
	Enter    time.Time
	Retained bool
	PID      int
	Checked  time.Time
	Last     time.Time
	Worked   bool
}

// NewUnaccepted は空の Unaccepted を作る。
func NewUnaccepted() *Unaccepted { return &Unaccepted{at: map[string]*unacceptedEntry{}} }

// Mark は id のペインに、at に Enter を送ったが受理を確かめられなかったことを覚える。覚えていれば（Retain 済みでも）上書きして、10 分の上限も戻る。
func (u *Unaccepted) Mark(id string, at time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.at[id] = &unacceptedEntry{enter: at}
}

// Clear は id のペインの記録を消す。
func (u *Unaccepted) Clear(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.at, id)
}

// Release は id のペインが Retain 済みなら記録を消す（shell に戻った・ペインが消えたとき。Issue #115）。Retain していないペインは何もしない。
func (u *Unaccepted) Release(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if e, ok := u.at[id]; ok && e.retained != 0 {
		delete(u.at, id)
	}
}

// Since は id のペインが受理を確かめられなかった Enter の時刻。無ければ ok=false。
func (u *Unaccepted) Since(id string) (at time.Time, ok bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.at[id]
	if !ok {
		return time.Time{}, false
	}
	return e.enter, true
}

// Retain は id のペインを、10 分の上限なしで見続ける対象にする（件を送ったあとも作業が続くかを見るため。Issue #115）。
// 記録が無いペイン（受理を確かめられたペイン）は何もしない。覚えたペインが maxRetained を超えたら、Retain が最も古いものの記録を消す。
func (u *Unaccepted) Retain(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.at[id]
	if !ok {
		return
	}
	u.seq++
	e.retained = u.seq
	var oldest string
	var n int
	for k, v := range u.at {
		if v.retained == 0 {
			continue
		}
		n++
		if oldest == "" || v.retained < u.at[oldest].retained {
			oldest = k
		}
	}
	if n > maxRetained {
		delete(u.at, oldest)
	}
}

// probe は id のペインのログの確認の記録。無ければ ok=false。
func (u *Unaccepted) probe(id string) (p logProbe, ok bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.at[id]
	if !ok {
		return logProbe{}, false
	}
	return logProbe{Enter: e.enter, Retained: e.retained != 0, PID: e.pid, Checked: e.checked, Last: e.last, Worked: e.worked}, true
}

// setProbe は id のペインのログを読んだ結果を覚える。記録が無ければ何もしない。
func (u *Unaccepted) setProbe(id string, pid int, checked, last time.Time, worked bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if e, ok := u.at[id]; ok {
		e.pid, e.checked, e.last, e.worked = pid, checked, last, worked
	}
}
