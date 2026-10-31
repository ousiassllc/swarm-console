package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// サーバー停止: コンテキストのキャンセルで、進行中の操作（貼り付け → 1 秒 → Enter）が最後まで終わってから止まる。
// 止まったあとは新しい接続を受け付けない。
func TestGracefulShutdownWaitsForInFlightOperation(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewAgent("omp", "idle")
	h.WaitState(id, "idle")
	type outcome struct {
		res harness.Result
		err error
	}
	done := make(chan outcome, 1)
	c := h.Ops()
	go func() {
		r, err := c.TryCall("send_text", map[string]any{"pane": id, "text": "停止中の操作"})
		done <- outcome{r, err}
	}()
	// 貼り付けが届いた（＝操作の途中）ところで停止を始める。
	h.WaitEvents(id, "貼り付けが届く", func(ev []fakeEvent) bool { return len(ev) > 0 })
	h.Cancel()

	select {
	case o := <-done:
		if o.err != nil || o.res.IsError || o.res.Map(t)["accepted"] != true {
			t.Errorf("進行中の操作が完了していない: %v %s", o.err, o.res.Text)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("進行中の操作が終わらない")
	}
	if err := h.Wait(60 * time.Second); err != nil {
		t.Errorf("Run の戻り値 = %v, want nil", err)
	}
	if ev := h.Events(id); len(ev) != 2 || ev[1].Kind != "SUBMIT" {
		t.Errorf("操作が最後まで実行されていない: %+v", ev)
	}
	if conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", h.Addr); err == nil {
		_ = conn.Close()
		t.Error("停止後も接続できる")
	}
}

// 進行中の操作が無ければすぐ止まる（サンプラーも止まる）。
func TestShutdownIsPromptWhenIdle(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	h.Ops().Must("panes_status", nil)
	start := time.Now()
	if err := h.Stop(); err != nil {
		t.Errorf("Run = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("停止に %s かかった", d)
	}
}
