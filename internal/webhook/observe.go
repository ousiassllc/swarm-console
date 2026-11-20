package webhook

import (
	"encoding/json"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// vanishAfter はペインが続けてこの回数スナップショットに無かったら、そのペインの回を捨てる（pane の消えた判定と同じ 2 回）。
const vanishAfter = 2

// Observe はスナップショット 1 つ分を処理する。pane.SnapshotObserver。通信は別 goroutine で行い、すぐ戻る。
// ペインが 1 つも無いスナップショット（tmux に接続できない・一覧が空）は、回の判定に使わない（tmux の起動時刻だけは見る）。
func (n *Notifier) Observe(snap *pane.Snapshot) {
	if snap == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	dirty := n.syncServerLocked(snap.ServerStart)
	if len(snap.Panes) > 0 {
		if n.trackLocked(snap) {
			dirty = true
		}
	}
	if n.syncWatchLocked() {
		dirty = true
	}
	if dirty {
		n.saveLocked() // 項目を作ったときも、ここで台帳に書いてから送りが始まる（送る goroutine は n.mu を待つ）
	}
}

// syncServerLocked は tmux サーバーの起動時刻を台帳と突き合わせる。変わっていたら再起動なので、ペインの回と見張りをすべて捨てる
// （ペイン ID は使い回される）。outbox の項目は残す。台帳が変わったら true。
func (n *Notifier) syncServerLocked(start string) bool {
	if start == "" || start == n.serverStart {
		return false
	}
	if n.serverStart != "" {
		clear(n.entries)
		clear(n.asked)
		clear(n.absent)
		n.dropWatchLocked()
	}
	n.serverStart = start
	return true
}

// trackLocked は全ペインの回を進め、続けて消えたペインの回を捨てる。台帳が変わったら true。
func (n *Notifier) trackLocked(snap *pane.Snapshot) bool {
	dirty := false
	present := make(map[string]bool, len(snap.Panes))
	for i := range snap.Panes {
		p := &snap.Panes[i]
		present[p.ID] = true
		if n.step(snap, p) {
			dirty = true
		}
	}
	for id := range n.absent {
		if present[id] || n.untracked(id) {
			delete(n.absent, id)
		}
	}
	vanish := func(id string) {
		if present[id] {
			return
		}
		if n.absent[id]++; n.absent[id] >= vanishAfter {
			delete(n.entries, id) // 消えたペイン（tmux の ID は再起動で使い回されるので、残さない）
			delete(n.asked, id)
			if w, ok := n.watch[id]; ok && n.o.Unaccepted != nil {
				n.o.Unaccepted.Drop(id, w.Enter) // 消えたペインの見張り（台帳から読み戻したものを含む）は捨てる（Issue #118）
			}
			delete(n.watch, id)
			delete(n.absent, id)
			dirty = true
		}
	}
	for id := range n.entries {
		vanish(id)
	}
	for id := range n.asked {
		if n.entries[id] == nil { // 回も持つペインは、上で数えた
			vanish(id)
		}
	}
	for id := range n.watch {
		if n.entries[id] == nil && n.asked[id] == "" {
			vanish(id)
		}
	}
	return dirty
}

// untracked は id のペインが、回・asking の回・見張りのどれも持たないか。
func (n *Notifier) untracked(id string) bool {
	_, watched := n.watch[id]
	return n.entries[id] == nil && n.asked[id] == "" && !watched
}

// dropWatchLocked は台帳の見張りをすべて捨てる（tmux サーバーが再起動して、ペイン ID が別のペインを指すようになったとき）。
// Unaccepted のうち、台帳の記録そのもの（同じ Enter の時刻）だけを消し、再起動のあとに受けた新しい指示の記録は残す。
func (n *Notifier) dropWatchLocked() {
	if n.o.Unaccepted != nil {
		for id, w := range n.watch {
			n.o.Unaccepted.Drop(id, w.Enter)
		}
	}
	clear(n.watch)
}

// syncWatchLocked は Unaccepted（操作とスナップショットの組み立てが書く）の見張りの記録を、台帳に書く写しへ取り込む。変わっていたら true。
// 受理を確かめられなかった指示の記録がサーバーの再起動で消えないようにするため（Issue #118。P-108）。
func (n *Notifier) syncWatchLocked() bool {
	if n.o.Unaccepted == nil {
		return false
	}
	cur := n.o.Unaccepted.Export()
	if len(cur) == len(n.watch) {
		same := true
		for id, w := range cur {
			if old, ok := n.watch[id]; !ok || !old.same(w) {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}
	clear(n.watch)
	for id, w := range cur {
		n.watch[id] = watchOf(w)
	}
	return true
}

// step は 1 ペインの回（pane.idle_report）と asking の回（pane.asking）を進める。台帳が変わったら true。
func (n *Notifier) step(snap *pane.Snapshot, p *pane.PaneState) bool {
	asked := n.stepAsking(snap, p)
	round := n.stepRound(snap, p)
	return asked || round
}

// stepAsking は asking の回を進める。asking を見始めたとき（その回の最初）に 1 件作る。
//
//	asking              → その回で作っていなければ項目を作って送る。同じ回では、質問が変わっても・何度見ても作らない
//	unknown             → 変えない（一時的な判定不能をはさんでも、同じ回）
//	それ以外（working・idle・stalled・shell） → 回を終える。次の asking は新しい回
func (n *Notifier) stepAsking(snap *pane.Snapshot, p *pane.PaneState) bool {
	switch p.State {
	case agent.StateAsking:
		if _, ok := n.asked[p.ID]; ok {
			return false
		}
		id := askEventID(p.Session, p.ID, snap.At)
		n.asked[p.ID] = id
		n.enqueueAskingLocked(p, id, snap.At)
		return true
	case agent.StateUnknown:
		return false
	}
	if _, ok := n.asked[p.ID]; !ok {
		return false
	}
	delete(n.asked, p.ID)
	return true
}

// enqueueAskingLocked は pane.asking の本文を 1 回だけ作って outbox に入れ、送りを始める（台帳へは Observe が書いてから送りが始まる）。
func (n *Notifier) enqueueAskingLocked(p *pane.PaneState, id string, at time.Time) {
	if _, ok := n.o.Config.Target(p.Session); !ok {
		return // このセッションには送り先が無い。何も作らず、何も送らない（回は作った扱いで、同じ回では作り直さない）
	}
	body, err := json.Marshal(newAskingPayload(p, id, at))
	if err != nil {
		n.o.Logger.Error("webhook の本文を作れませんでした", "event_id", id)
		return
	}
	it := &item{EventID: id, Payload: body}
	n.outbox[id] = it
	n.startItemLocked(it)
}

// stepRound は 1 ペインの idle の回（pane.idle_report）を進める。台帳が変わったら true。
//
//	working・stalled → 回を始める（そのときの最終報告を Base に覚える）。done の回のあとでも、新しい回。
//	                   done の回のあとは、件を作ったときの最終報告（Sent）を Base にする（その後に書き直されていれば、この回の idle で送る）
//	idle（回あり）   → 待ちに移る。最終報告が Base から変わっていれば項目を作って送る（回は done）
//	shell            → 回を捨てる
//	asking・unknown  → 変えない（質問や一時的な判定不能をはさんでも、同じ回）
func (n *Notifier) stepRound(snap *pane.Snapshot, p *pane.PaneState) bool {
	e := n.entries[p.ID]
	switch p.State {
	case agent.StateWorking, agent.StateStalled:
		if e != nil && e.Phase == phaseWorking {
			return n.rebaseLocked(p, e)
		}
		n.entries[p.ID] = n.newRound(p, e)
		return true
	case agent.StateShell:
		if e == nil {
			return false
		}
		delete(n.entries, p.ID)
		return true
	case agent.StateIdle:
		if e == nil || e.Phase == phaseDone {
			return false
		}
		dirty := false
		if e.Phase == phaseWorking {
			e.Phase, e.ChangedAt, e.EventID = phaseWaiting, snap.At, eventID(p.Session, p.ID, snap.At)
			dirty = true
		}
		if n.rebaseLocked(p, e) { // 違うファイルになったので、比べずに取り直す
			return true
		}
		if cur := n.stat(p); cur.Exists && cur != e.Base {
			n.completeRoundLocked(p, e, cur)
			return true
		}
		return dirty
	}
	return false
}

// newRound は working を見て回を始める。前の回が done（件を作った）で、同じパスの最終報告なら、そのとき（Sent）を Base にする。
// working を最初に見る前に最終報告が書き直されていても、「書き直された」と読むため（Issue #115）。
// Sent の無い done（#115 より前の台帳）・前の回が無い・パスが違うときは、今の最終報告を Base にする。
func (n *Notifier) newRound(p *pane.PaneState, prev *entry) *entry {
	path := n.reportPath(p)
	base := n.stat(p)
	if prev != nil && prev.Phase == phaseDone && prev.Sent != nil && prev.BasePath == path {
		base = *prev.Sent
	}
	return &entry{Phase: phaseWorking, Base: base, BasePath: path}
}

// rebaseLocked は最終報告のパスが Base を取ったときと違えば（ペインの位置がずれたとき）、新しいパスで Base を取り直す。
// 違うファイル同士を比べて「書かれた」と見間違えないため。取り直したら true。
func (n *Notifier) rebaseLocked(p *pane.PaneState, e *entry) bool {
	path := n.reportPath(p)
	if path == e.BasePath {
		return false
	}
	e.Base, e.BasePath = n.stat(p), path
	return true
}

// completeRoundLocked は最終報告が書かれた回を終える。本文を 1 回だけ作って outbox に入れ、回を done にする
// （同じ idle の回では二度と作らない。次の working で新しい回）。cur は「書けた」と見た最終報告で、次の回の Base になる（Sent）。
// 台帳へは Observe が書いてから、送りが始まる。
func (n *Notifier) completeRoundLocked(p *pane.PaneState, e *entry, cur reportStat) {
	e.Phase, e.Sent = phaseDone, &cur
	if _, ok := n.o.Config.Target(p.Session); !ok {
		return // このセッションには送り先が無い（[webhook] もセッションの表も無い、または url が空）。何も作らず、何も送らない
	}
	body, err := json.Marshal(newPayload(n.o.ReportDir, p, e))
	if err != nil {
		n.o.Logger.Error("webhook の本文を作れませんでした", "event_id", e.EventID)
		return
	}
	it := &item{EventID: e.EventID, Payload: body}
	n.outbox[it.EventID] = it
	n.startItemLocked(it)
	if n.o.Unaccepted != nil {
		// 画面が止まった omp は、件を送ったあとも作業が続くかをログで見続ける（10 分の上限を外す。Issue #115。P-107）。
		n.o.Unaccepted.Retain(p.ID)
	}
}

func (n *Notifier) reportPath(p *pane.PaneState) string {
	return pane.ReportPath(n.o.ReportDir, p.ReportPos())
}

// stat は最終報告ファイルの今の様子。無い・通常のファイルでない（リンク）なら「無し」。
func (n *Notifier) stat(p *pane.PaneState) reportStat {
	fi, err := n.o.FS.Lstat(n.reportPath(p))
	if err != nil || !fi.Mode().IsRegular() {
		return reportStat{}
	}
	return reportStat{Exists: true, ModNS: fi.ModTime().UnixNano(), Size: fi.Size()}
}
