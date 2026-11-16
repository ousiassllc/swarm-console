package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
)

// Issue #93: 再送の途中の項目は、ペインが working に戻って新しい回が始まっても、再起動しても、同じ本文・同じ event_id で届く。
// ペインのメモが途中で変わっても、本文（と署名）は変わらない。新しい回は、あとで別の event_id で届く。
func TestRetryingItemSurvivesNewRoundAndRestartWithSameBody(t *testing.T) {
	h := newHarness(t)
	cancel := h.start()
	h.setStatus(http.StatusServiceUnavailable)
	h.onSleep = cancel
	h.idleWithReport() // 1 回目が失敗し、再送の待ちで終了。項目は outbox に残る
	h.onSleep = nil
	if len(h.n.outbox) != 1 || len(h.received()) != 1 {
		t.Fatalf("outbox = %d 件, POST = %d 回, want 1・1", len(h.n.outbox), len(h.received()))
	}

	h.note = &notes.Summary{From: "べつ", Repo: "x/y", Items: []string{"#1"}, Next: "メモを変えた"}
	h.observe(agent.StateWorking) // 新しい回。送り途中の項目には触れない
	if len(h.n.outbox) != 1 {
		t.Fatalf("新しい回で outbox が変わりました: %d 件", len(h.n.outbox))
	}

	h.setStatus(http.StatusOK)
	h.restart() // 再起動。台帳の項目を続きから送る
	hits := h.received()
	if len(hits) != 2 {
		t.Fatalf("POST = %d 回, want 2", len(hits))
	}
	if !bytes.Equal(hits[0].body, hits[1].body) {
		t.Errorf("本文が変わっています:\n%s\n%s", hits[0].body, hits[1].body)
	}
	for _, key := range []string{headerEventID, headerSignature} {
		if hits[0].header.Get(key) != hits[1].header.Get(key) || hits[0].header.Get(key) == "" {
			t.Errorf("%s が変わっています: %q → %q", key, hits[0].header.Get(key), hits[1].header.Get(key))
		}
	}
	if h.payload(1).Note.Next != "PR を出す" {
		t.Errorf("本文のメモが、あとのメモに替わっています: %q", h.payload(1).Note.Next)
	}

	// 新しい回（再起動の前に始まった）は、レポートが更新されたあとの idle で、別の event_id で届く。
	h.report(2000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 3 {
		t.Fatalf("新しい回が届いていません: %d 回", len(h.received()))
	}
	if h.payload(2).EventID == h.payload(0).EventID || h.payload(2).Note.Next != "メモを変えた" {
		t.Errorf("新しい回の payload: %+v", h.payload(2))
	}
}

// Issue #93: tmux の起動時刻が変わったら、ペインの回はすべて捨てる（ペイン ID が使い回される）。送る約束をした項目は残す。
func TestTmuxRestartDropsPaneEntriesButKeepsOutbox(t *testing.T) {
	h := newHarness(t)
	h.srvStart = "100"
	cancel := h.start()
	h.setStatus(http.StatusServiceUnavailable)
	h.onSleep = cancel
	h.idleWithReport() // 項目が outbox に残る
	h.onSleep = nil

	h.observe(agent.StateWorking) // 新しい回
	h.srvStart = "200"            // tmux が再起動した
	h.report(2000)
	h.observe(agent.StateIdle)
	if len(h.n.entries) != 0 {
		t.Errorf("tmux の再起動のあとにペインの回が残っています: %+v", h.n.entries)
	}
	if len(h.n.outbox) != 1 || h.n.serverStart != "200" {
		t.Errorf("outbox = %d 件, server_start = %q, want 1・200", len(h.n.outbox), h.n.serverStart)
	}
	if got := len(h.received()); got != 1 {
		t.Fatalf("捨てた回で送りました: %d 回", got)
	}

	h.setStatus(http.StatusOK)
	h.restart()
	if got := len(h.received()); got != 2 {
		t.Fatalf("項目が届いていません: %d 回", got)
	}
	if h.n.serverStart != "200" {
		t.Errorf("台帳の server_start = %q, want 200", h.n.serverStart)
	}
}

// 起動時刻が読めない（空）・台帳にまだ無いときは、回を捨てない。
func TestEmptyOrFirstServerStartKeepsPaneEntries(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.observe(agent.StateWorking) // 起動時刻なし
	h.srvStart = "100"            // 台帳にはまだ無い → 覚えるだけ
	h.observe(agent.StateWorking)
	h.srvStart = "" // 読めなかった
	h.observe(agent.StateWorking)
	if _, ok := h.n.entries["%3"]; !ok || h.n.serverStart != "100" {
		t.Fatalf("回が消えた・起動時刻が違う: %+v server_start=%q", h.n.entries, h.n.serverStart)
	}
	h.report(1000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 1 {
		t.Fatalf("POST = %d 回, want 1", len(h.received()))
	}
}

// ペインが 1 つも無いスナップショットでも、起動時刻が変わったことは見て、回を捨てる。
func TestServerStartChangeIsSeenOnEmptySnapshot(t *testing.T) {
	h := newHarness(t)
	h.srvStart = "100"
	h.start()
	h.observe(agent.StateWorking)
	h.srvStart = "200"
	h.snap() // 一覧が空
	if len(h.n.entries) != 0 || h.n.serverStart != "200" {
		t.Fatalf("entries=%+v server_start=%q", h.n.entries, h.n.serverStart)
	}
}

// Issue #93: 送った回数（Attempts）は再起動をまたいで共有する。上限 3 で 2 回失敗して再起動したら、もう 1 回だけ試して諦める。
func TestAttemptsAreSharedAcrossRestart(t *testing.T) {
	h := newHarness(t)
	cancel := h.start()
	h.setStatus(http.StatusServiceUnavailable)
	sleeps := 0
	h.onSleep = func() {
		if sleeps++; sleeps == 2 {
			cancel() // 2 回失敗したあとの待ちで終了
		}
	}
	h.idleWithReport()
	if got := len(h.received()); got != 2 {
		t.Fatalf("再起動の前の POST = %d 回, want 2", got)
	}
	if len(h.n.outbox) != 1 {
		t.Fatalf("outbox = %d 件, want 1", len(h.n.outbox))
	}
	for _, it := range h.n.outbox {
		if it.Attempts != 2 {
			t.Errorf("Attempts = %d, want 2", it.Attempts)
		}
	}
	if strings.Contains(h.logs.String(), `"level":"ERROR"`) {
		t.Fatalf("中断を失敗として記録しています:\n%s", h.logs.String())
	}

	h.restart() // 残りは 1 回だけ
	if got := len(h.received()); got != 3 {
		t.Fatalf("再起動後の POST の合計 = %d 回, want 3（上限）", got)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, `"attempts":3`) {
		t.Errorf("諦めたことがログにありません:\n%s", logs)
	}
	// 待ちは、再起動の前に 2 回（30s・60s）。再起動後は、続きの前に 2 回目の失敗の分（60s）を待つ。
	if got, want := h.sleeps(), []time.Duration{30 * time.Second, 60 * time.Second, 60 * time.Second}; len(h.n.outbox) != 0 || !reflect.DeepEqual(got, want) {
		t.Errorf("outbox = %d 件, 待ち = %v, want 0・%v", len(h.n.outbox), got, want)
	}
}

// 読み込んだ項目がすでに上限まで送っていたら、送らずに諦める（理由 attempts_exhausted）。
func TestLoadedItemWithExhaustedAttemptsGivesUpWithoutPosting(t *testing.T) {
	h := newHarness(t)
	body, err := json.Marshal(Payload{Event: EventPaneIdleReport, EventID: "evt_x", Session: "0", Pane: "T2", Note: Note{Items: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	led := emptyLedger()
	led.Outbox["evt_x"] = &item{EventID: "evt_x", Payload: body, Attempts: h.cfg.MaxAttempts}
	if err := saveLedger(h.state, led); err != nil {
		t.Fatal(err)
	}
	h.start()
	h.n.wg.Wait()
	if len(h.received()) != 0 {
		t.Fatal("上限まで送った項目をまた送りました")
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, `"reason":"attempts_exhausted"`) {
		t.Errorf("諦めたことがログにありません:\n%s", logs)
	}
	if len(h.n.outbox) != 0 {
		t.Errorf("outbox = %d 件, want 0", len(h.n.outbox))
	}
}

// Start の前に作った項目は、Start まで待つ。台帳へは、送り始める前に書いてある。
func TestItemCreatedBeforeStartWaitsForStart(t *testing.T) {
	h := newHarness(t)
	h.build()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)
	if len(h.received()) != 0 || len(h.n.outbox) != 1 {
		t.Fatalf("Start の前に送りました: POST = %d 回, outbox = %d 件", len(h.received()), len(h.n.outbox))
	}
	data, err := os.ReadFile(h.state)
	if err != nil || !strings.Contains(string(data), `"outbox"`) || !strings.Contains(string(data), "evt_") {
		t.Fatalf("台帳に項目が無い: %v\n%s", err, data)
	}
	h.begin()
	h.n.wg.Wait()
	if len(h.received()) != 1 || len(h.n.outbox) != 0 {
		t.Fatalf("POST = %d 回, outbox = %d 件, want 1・0", len(h.received()), len(h.n.outbox))
	}
}

// 壊れた台帳・古い版（1）の台帳は、警告を出して空から始める。
func TestUnreadableOrOldLedgerStartsEmptyWithWarning(t *testing.T) {
	for name, content := range map[string]string{
		"壊れた JSON": "{not json",
		"古い版":      `{"version":1,"panes":{"%3":{"phase":"waiting","state_changed_at":"2026-11-16T09:00:00Z","report_base":{"exists":false}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if err := os.WriteFile(h.state, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			h.start()
			if len(h.n.entries) != 0 || len(h.n.outbox) != 0 {
				t.Errorf("空から始まっていません: entries=%d outbox=%d", len(h.n.entries), len(h.n.outbox))
			}
			if !strings.Contains(h.logs.String(), "webhook の台帳を読めませんでした") {
				t.Errorf("警告がありません:\n%s", h.logs.String())
			}
		})
	}
}

// 台帳（版 2）の形: server_start・panes（phase・event_id・state_changed_at・report_base・report_path）・outbox（event_id・payload・attempts）。
func TestLedgerShape(t *testing.T) {
	h := newHarness(t)
	h.srvStart = "100"
	cancel := h.start()
	h.setStatus(http.StatusServiceUnavailable)
	h.onSleep = cancel
	h.idleWithReport()

	data, err := os.ReadFile(h.state)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Version     int    `json:"version"`
		ServerStart string `json:"server_start"`
		Panes       map[string]struct {
			Phase     string `json:"phase"`
			EventID   string `json:"event_id"`
			ChangedAt string `json:"state_changed_at"`
			Base      struct {
				Exists bool `json:"exists"`
			} `json:"report_base"`
			ReportPath string `json:"report_path"`
		} `json:"panes"`
		Outbox map[string]struct {
			EventID  string  `json:"event_id"`
			Payload  Payload `json:"payload"`
			Attempts int     `json:"attempts"`
		} `json:"outbox"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	p, ok := f.Panes["%3"]
	if f.Version != 2 || f.ServerStart != "100" || !ok || p.Phase != phaseDone || p.EventID == "" || p.ChangedAt == "" || p.ReportPath != reportPath {
		t.Fatalf("panes・版が違います: %s", data)
	}
	it, ok := f.Outbox[p.EventID]
	if !ok || it.EventID != p.EventID || it.Attempts != 1 || it.Payload.Event != EventPaneIdleReport || it.Payload.EventID != p.EventID {
		t.Fatalf("outbox が違います: %s", data)
	}
	if fi, err := os.Stat(h.state); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("台帳の権限が 600 でない: %v %v", fi, err)
	}
}
