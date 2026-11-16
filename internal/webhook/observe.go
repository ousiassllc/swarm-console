package webhook

import (
	"encoding/json"

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
	if dirty {
		n.saveLocked() // 項目を作ったときも、ここで台帳に書いてから送りが始まる（送る goroutine は n.mu を待つ）
	}
}

// syncServerLocked は tmux サーバーの起動時刻を台帳と突き合わせる。変わっていたら再起動なので、ペインの回をすべて捨てる
// （ペイン ID は使い回される）。outbox の項目は残す。台帳が変わったら true。
func (n *Notifier) syncServerLocked(start string) bool {
	if start == "" || start == n.serverStart {
		return false
	}
	if n.serverStart != "" {
		clear(n.entries)
		clear(n.absent)
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
		if present[id] || n.entries[id] == nil {
			delete(n.absent, id)
		}
	}
	for id := range n.entries {
		if present[id] {
			continue
		}
		if n.absent[id]++; n.absent[id] >= vanishAfter {
			delete(n.entries, id) // 消えたペイン（tmux の ID は再起動で使い回されるので、残さない）
			delete(n.absent, id)
			dirty = true
		}
	}
	return dirty
}

// step は 1 ペインの回を進める。台帳が変わったら true。
//
//	working・stalled → 回を始める（そのときの最終報告を Base に覚える）。done の回のあとでも、新しい回
//	idle（回あり）   → 待ちに移る。最終報告が Base から変わっていれば項目を作って送る（回は done）
//	shell            → 回を捨てる
//	asking・unknown  → 変えない（質問や一時的な判定不能をはさんでも、同じ回）
func (n *Notifier) step(snap *pane.Snapshot, p *pane.PaneState) bool {
	e := n.entries[p.ID]
	switch p.State {
	case agent.StateWorking, agent.StateStalled:
		if e != nil && e.Phase == phaseWorking {
			return n.rebaseLocked(p, e)
		}
		n.entries[p.ID] = n.newRound(p)
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
			n.completeRoundLocked(p, e)
			return true
		}
		return dirty
	}
	return false
}

func (n *Notifier) newRound(p *pane.PaneState) *entry {
	return &entry{Phase: phaseWorking, Base: n.stat(p), BasePath: n.reportPath(p)}
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
// （同じ idle の回では二度と作らない。次の working で新しい回）。台帳へは Observe が書いてから、送りが始まる。
func (n *Notifier) completeRoundLocked(p *pane.PaneState, e *entry) {
	e.Phase = phaseDone
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
