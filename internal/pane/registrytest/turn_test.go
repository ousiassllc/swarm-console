package registrytest

import (
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Issue #118（P-108）: 受理を確かめられなかったペインは、会話の記録の最後が番の途中（長いコマンドの実行中を含む）なら、
// ログにモデルの呼び出しの行が何分も出なくても working と読む。番が終われば idle に戻り、動いていないペインを working にしない。

// 長いコマンドの間（モデルの呼び出しの行が 10 分以上出ない）も working。件を送る前（Retain 前）でも 10 分で見張りを捨てない。
func TestUnacceptedPaneStaysWorkingWhileTurnIsOpen(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	g.env.Clock.Advance(time.Second)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), true) // ツールの呼び出し（長いコマンド）で止まったまま
	for waited := time.Duration(0); waited < 40*time.Minute; waited += 5 * time.Second {
		g.env.Clock.Advance(5 * time.Second)
		// 画面が 15 分変わらなければ、作業中と読んだ状態は stalled になる（回の判定では working と同じ扱い）
		if got := g.state(t); got != agent.StateWorking && got != agent.StateStalled {
			t.Fatalf("長いコマンドの間（%s 後）に %s, want working（または stalled）", waited, got)
		}
	}
	if _, ok := g.un.Since("%0"); !ok {
		t.Fatal("番の途中なのに記録を捨てた")
	}
}

// 番が終わったら idle。そこから 10 分作業の跡が無ければ記録も捨てる（誤って working にし続けない）。
func TestUnacceptedPaneGoesIdleWhenTurnEnds(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	g.env.Clock.Advance(time.Second)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), true)
	if got := g.state(t); got != agent.StateWorking {
		t.Fatalf("番の途中: %s, want working", got)
	}
	g.env.Clock.Advance(30 * time.Minute)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), false) // 最終の応答で終わった
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("番が終わったのに %s, want idle", got)
	}
	g.env.Clock.Advance(9 * time.Minute)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("%s", got)
	}
	if _, ok := g.un.Since("%0"); !ok {
		t.Fatal("番が終わって 9 分では記録を捨てない")
	}
	g.env.Clock.Advance(2 * time.Minute)
	g.state(t)
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("番が終わって 10 分以上たったのに記録が残っている")
	}
}

// Enter より前の会話の記録（前の番の途中で止まったままのもの）では working にしない。実際に作業していない。
func TestTurnBeforeTheEnterIsNotCounted(t *testing.T) {
	g := newLogRig(t)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), true)
	g.env.Clock.Advance(time.Second)
	g.un.Mark("%0", g.env.Clock.Now())
	for range 5 {
		g.env.Clock.Advance(time.Minute)
		if got := g.state(t); got != agent.StateIdle {
			t.Fatalf("Enter より前の記録で working にした: %s", got)
		}
	}
}

// 記録の無いペイン（受理を確かめられたペイン）は、会話の記録が番の途中でも画面どおり。
func TestPaneWithoutUnacceptedMarkIgnoresTurn(t *testing.T) {
	g := newLogRig(t)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), true)
	g.env.Clock.Advance(time.Second)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("記録が無いのに会話の記録で working にした: %s", got)
	}
}

// 別のプロセス（pid）になったペインは、台帳から引き継いだ記録でも working にせず、捨てる。
func TestRestoredWatchOfAnotherProcessIsDropped(t *testing.T) {
	g := newLogRig(t)
	now := g.env.Clock.Now()
	g.un.Restore(map[string]pane.Watch{"%0": {Enter: now, Retained: 1, PID: logPID + 50}})
	g.env.Clock.Advance(time.Second)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), true)
	if got := g.state(t); got != agent.StateIdle {
		t.Fatalf("別のプロセスの記録で working にした: %s", got)
	}
	if _, ok := g.un.Since("%0"); ok {
		t.Fatal("別のプロセスの記録が残っている")
	}
}

// Export・Restore で、見張りの記録（Enter の時刻・Retain の印・pid）が往復する。Restore は、すでにある記録を上書きしない。
func TestUnacceptedExportRestoreRoundTrip(t *testing.T) {
	g := newLogRig(t)
	g.un.Mark("%0", g.env.Clock.Now())
	g.env.Clock.Advance(time.Second)
	g.probe.SetTurn(logPID, g.env.Clock.Now(), true)
	g.state(t) // pid を覚える
	g.un.Retain("%0")
	exp := g.un.Export()
	w, ok := exp["%0"]
	if !ok || w.PID != logPID || w.Retained == 0 || w.Enter.IsZero() {
		t.Fatalf("Export = %+v", exp)
	}

	un2 := pane.NewUnaccepted()
	newer := g.env.Clock.Now().Add(time.Hour)
	un2.Mark("%1", newer)
	un2.Restore(map[string]pane.Watch{"%0": w, "%1": {Enter: w.Enter}})
	if at, ok := un2.Since("%0"); !ok || !at.Equal(w.Enter) {
		t.Fatalf("Restore した %v %v", at, ok)
	}
	if at, _ := un2.Since("%1"); !at.Equal(newer) {
		t.Fatalf("すでにある記録を上書きした: %v", at)
	}
	un2.Drop("%1", w.Enter) // 別の Enter の記録は消さない
	if _, ok := un2.Since("%1"); !ok {
		t.Fatal("違う Enter の記録を Drop で消した")
	}
	un2.Drop("%0", w.Enter)
	if _, ok := un2.Since("%0"); ok {
		t.Fatal("同じ Enter の記録を Drop で消せない")
	}
}
