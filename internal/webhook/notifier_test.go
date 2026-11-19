package webhook

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// 再起動しても同じ回は二重に送らない。台帳を読んだ新しい Notifier が、done の回のペインを idle で見続けても（レポートが更新されても）送らない。
func TestRestartDoesNotResendSameRound(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(h.received()))
	}

	h.restart() // 再起動（同じ台帳）
	h.observe(agent.StateIdle)
	h.report(3000)
	h.observe(agent.StateIdle)
	h.observe(agent.StateIdle)
	if got := len(h.received()); got != 1 {
		t.Fatalf("再起動のあとに同じ回を送り直しました: %d 回", got)
	}
}

// idle になったがレポートがまだのうちに再起動しても、待ちを引き継ぎ、書かれたら同じ event_id で 1 回送る。
func TestRestartWhileWaitingForReportContinues(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking)
	h.observe(agent.StateIdle)
	changedAt := h.at

	h.restart()
	h.observe(agent.StateIdle)
	if len(h.received()) != 0 {
		t.Fatal("レポートが無いのに送りました")
	}
	h.report(1000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(h.received()))
	}
	if got, want := h.payload(0).EventID, eventID("0", "%3", changedAt); got != want {
		t.Errorf("event_id = %s, want %s（再起動の前の回と同じ）", got, want)
	}
}

// ペインが 1 回スナップショットに無いだけでは回を捨てない（取得の一時的な欠け）。続けて 2 回無ければ捨てる
// （tmux のペイン ID は使い回される）。
func TestVanishedPaneDropsRoundOnlyAfterTwoSnapshots(t *testing.T) {
	h := newHarness(t)
	h.start()
	other := h.paneAt("%9", "0:T3", "T3", agent.StateIdle)

	h.observe(agent.StateWorking)
	h.snap(other) // 1 回だけ無い
	if _, ok := h.n.entries["%3"]; !ok {
		t.Fatal("1 回無かっただけで回を捨てました")
	}
	h.report(1000)
	h.observe(agent.StateIdle) // 戻ってきた。同じ回で送れる
	if got := len(h.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1（1 回の欠けをまたいで同じ回を続ける）", got)
	}

	h.observe(agent.StateWorking) // 新しい回
	h.snap(other)
	h.snap(other) // 続けて 2 回無い
	if _, ok := h.n.entries["%3"]; ok {
		t.Fatal("消えたペインの回が残っています")
	}
	h.report(2000)
	h.observe(agent.StateIdle) // 回を持たない idle では送らない
	if got := len(h.received()); got != 1 {
		t.Fatalf("消えた回で送りました: %d 回", got)
	}
}

// 欠けの回数は「続けて」数える。間に戻ってきたら数え直す。
func TestVanishCounterResetsWhenPaneReturns(t *testing.T) {
	h := newHarness(t)
	h.start()
	other := h.paneAt("%9", "0:T3", "T3", agent.StateIdle)
	h.observe(agent.StateWorking)
	h.snap(other)
	h.observe(agent.StateWorking)
	h.snap(other)
	if _, ok := h.n.entries["%3"]; !ok {
		t.Fatal("間に戻ってきたのに、欠けを合算して回を捨てました")
	}
}
