package nativetest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/testutil"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// hookRig は rig に Notifier（本物）を観測者として付け、受け手（httptest）に届いた POST を集める。
type hookRig struct {
	*rig
	reports string
	ledger  string
	mu      sync.Mutex
	bodies  [][]byte
	cancel  context.CancelFunc
	wait    func()
}

func newHookRig(t *testing.T, lines int) *hookRig {
	t.Helper()
	h := &hookRig{reports: t.TempDir()}
	h.ledger = filepath.Join(t.TempDir(), "webhook-state.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, b)
		h.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	un := pane.NewUnaccepted()
	n, err := webhook.New(webhook.Options{
		Config:    config.Webhook{URL: config.Secret(srv.URL), MaxAttempts: 1, RetryInterval: time.Second, Timeout: 5 * time.Second},
		ReportDir: h.reports, StatePath: h.ledger, Version: "test", Unaccepted: un,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.wait = cancel, n.Start(ctx)
	t.Cleanup(func() { cancel(); h.wait() })
	h.rig = newRig(t, lines, n, pane.OSReportFS{}, un)
	h.env.Cfg.ReportDir = h.reports
	h.setClassic(classicSample(t, "idle.txt"))
	return h
}

// sent は作った項目がすべて送り終わる（台帳の outbox が空になる）まで、上限つきで待つ。
func (h *hookRig) sent(t *testing.T) {
	t.Helper()
	testutil.Eventually(t, "作った項目がすべて送り終わる（台帳の outbox が空になる）", func() bool {
		b, err := os.ReadFile(h.ledger)
		return err == nil && strings.Contains(string(b), `"outbox":{}`)
	})
}

// posted は送信が終わるのを待って、届いた POST の event の一覧を返す。
func (h *hookRig) posted(t *testing.T) []string {
	t.Helper()
	h.sent(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	var events []string
	for _, b := range h.bodies {
		var p struct {
			Event  string `json:"event"`
			PaneID string `json:"pane_id"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		events = append(events, p.Event+" "+p.PaneID)
	}
	return events
}

func (h *hookRig) tick(t *testing.T) pane.PaneState {
	t.Helper()
	h.env.Clock.Advance(5 * time.Second)
	n, _ := h.states()
	return n
}

// 受け入れ条件：tern の画面のまま作業して終わったとき、pane.idle_report が 1 回だけ飛ぶ。
func TestIdleReportPostedOnceOnTernScreen(t *testing.T) {
	h := newHookRig(t, 3) // idle
	if st := h.tick(t).State; st != agent.StateIdle {
		t.Fatalf("state = %s, want idle", st)
	}
	h.userSays("調べて報告して") // 作業が始まる（画面は変わらない。記録だけが進む）
	if st := h.tick(t).State; st != agent.StateWorking {
		t.Fatalf("state = %s, want working", st)
	}
	if err := os.WriteFile(filepath.Join(h.reports, "T1.md"), []byte("done\n"), 0o600); err != nil { // 最終報告
		t.Fatal(err)
	}
	h.tick(t)
	h.assistantStops() // 最終の応答で作業が終わる
	for range 4 {      // idle のまま何度見ても、重ねて送らない
		if st := h.tick(t).State; st != agent.StateIdle {
			t.Fatalf("state = %s, want idle", st)
		}
	}
	if got := h.posted(t); len(got) != 1 || got[0] != "pane.idle_report %0" {
		t.Fatalf("POST = %v, want pane.idle_report が 1 回だけ", got)
	}
	// 次の回（また作業して終わる）は、別の件として 1 回送る。
	h.userSays("もう 1 件")
	h.tick(t)
	if err := os.WriteFile(filepath.Join(h.reports, "T1.md"), []byte("done, again and longer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.assistantStops()
	h.tick(t)
	h.tick(t)
	if got := h.posted(t); len(got) != 2 {
		t.Fatalf("次の回の POST = %v, want 合計 2 回", got)
	}
}

// 質問で止まったら pane.asking が 1 回だけ飛ぶ。回答して作業に戻っても、重ねて送らない。
func TestAskingPostedOnceOnTernScreen(t *testing.T) {
	h := newHookRig(t, 4) // 指示を受けて作業中
	h.tick(t)
	h.appendFixture(6) // 質問が出る
	for range 3 {
		if st := h.tick(t).State; st != agent.StateAsking {
			t.Fatalf("state = %s, want asking", st)
		}
	}
	if got := h.posted(t); len(got) != 1 || got[0] != "pane.asking %0" {
		t.Fatalf("POST = %v, want pane.asking が 1 回だけ", got)
	}
	h.appendFixture(7) // 回答
	for range 3 {
		if st := h.tick(t).State; st != agent.StateWorking {
			t.Fatalf("state = %s, want working", st)
		}
	}
	if got := h.posted(t); len(got) != 1 {
		t.Fatalf("回答のあとに重ねて送った: %v", got)
	}
}
