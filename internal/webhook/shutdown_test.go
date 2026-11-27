package webhook

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/testutil"
)

// 通信の途中（POST を受け手が受けて、応答する前）で終了しても、その 1 回は送った回数に数えない。
// max_attempts = 1 でも、再起動後に 1 回やり直して届く（attempts_exhausted にも ERROR にもならない）。
func TestShutdownMidPostDoesNotBurnAttempt(t *testing.T) {
	h := newHarness(t)
	h.cfg.MaxAttempts = 1
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(closeRelease) // srv.Close の前に、止めた受け手を解放する（Cleanup は後に登録したものから動く）
	h.mu.Lock()
	h.intercept = func(http.ResponseWriter, *http.Request) bool {
		once.Do(func() { close(entered) })
		<-release
		return true
	}
	h.mu.Unlock()
	cancel := h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	done := make(chan struct{})
	go func() { // POST が止まっている間、observe（送信の終了待ち）は戻らない
		h.observe(agent.StateIdle)
		close(done)
	}()
	testutil.Recv(t, entered, "POST が受け手に届く")
	cancel() // 通信の途中で終了
	testutil.Recv(t, done, "observe が戻る")
	closeRelease()
	if strings.Contains(h.logs.String(), `"level":"ERROR"`) {
		t.Fatalf("中断を失敗として記録しています:\n%s", h.logs.String())
	}
	for _, it := range h.n.outbox {
		if it.Attempts != 0 {
			t.Errorf("中断した通信が回数に数えられています: Attempts = %d", it.Attempts)
		}
	}

	h.mu.Lock()
	h.intercept = nil
	h.mu.Unlock()
	h.restart() // 同じ台帳で再起動。1 回やり直して届く
	hits := h.received()
	if len(hits) != 2 {
		t.Fatalf("POST = %d 回, want 2（中断した 1 回と、やり直しの 1 回）", len(hits))
	}
	if string(hits[0].body) != string(hits[1].body) {
		t.Error("やり直しの本文が変わっています")
	}
	logs := h.logs.String()
	if strings.Contains(logs, `"level":"ERROR"`) || strings.Contains(logs, reasonExhausted) || !strings.Contains(logs, "webhook を送りました") {
		t.Errorf("やり直しが届いていません:\n%s", logs)
	}
	if len(h.n.outbox) != 0 {
		t.Errorf("outbox = %d 件, want 0", len(h.n.outbox))
	}
}

// 引き継いだ件は、送った回数に応じた間隔をあけてから送る（POST の前に待つ）。回数 2 なら RetryInterval の 2 倍。
func TestResumedItemWaitsBackoffBeforePosting(t *testing.T) {
	h := newHarness(t)
	body, err := json.Marshal(Payload{Event: EventPaneIdleReport, EventID: "evt_y", Session: "0", Pane: "T2", Note: Note{Items: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	led := emptyLedger()
	led.Outbox["evt_y"] = &item{EventID: "evt_y", Payload: body, Attempts: 2}
	if err := saveLedger(h.state, led); err != nil {
		t.Fatal(err)
	}
	var atSleep []int // 待ちの時点で届いていた POST の数
	h.onSleep = func() { atSleep = append(atSleep, len(h.received())) }
	h.start()
	h.n.wg.Wait()
	if got, want := h.sleeps(), []time.Duration{retryDelay(h.cfg.RetryInterval, 2)}; len(got) != 1 || got[0] != want[0] || want[0] != 60*time.Second {
		t.Fatalf("待ち = %v, want %v", got, want)
	}
	if len(atSleep) != 1 || atSleep[0] != 0 || len(h.received()) != 1 {
		t.Errorf("待ちは POST の前のはずです: 待ちの時点の POST = %v, 合計 = %d", atSleep, len(h.received()))
	}
}
