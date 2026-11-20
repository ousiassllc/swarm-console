package webhooktests

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// Issue #118（P-108）: 受理を確かめられなかった dispatch のあとで omp が長いコマンド（学習・CI 待ちなど）を実行し、
// モデルの呼び出しの行が 10 分以上出なくても、会話の記録（セッションの jsonl）の最後が番の途中なら working と読み、
// 最終報告が書かれて番が終わったら pane.idle_report が 1 回だけ飛ぶ。見張りは台帳に持ち、サーバーの再起動のあとも続く。

// newLongCommand は本番の配線（Notifier と Registry が同じ Unaccepted を持つ）の frozenScreen。
func newLongCommand(t *testing.T, r *receiver) *frozenScreen { return newRetainingScreen(t, r) }

// startLongCommand は dispatch が受理を確かめられず、omp がすぐ長いコマンドの実行に入ったところまで進める。ログにはモデルの呼び出しの行が 1 つも無い。
func startLongCommand(t *testing.T, f *frozenScreen) {
	t.Helper()
	f.see(time.Second)
	f.un.Mark("%0", f.env.Clock.Now())
	f.env.Clock.Advance(time.Second)
	f.probe.SetTurn(frozenPID, f.env.Clock.Now(), true) // 指示を受け、ツールの呼び出し（長いコマンド）で結果を待っている
	if got := f.see(5 * time.Second); !working(got) {
		t.Fatalf("長いコマンドの実行中なのに %s", got)
	}
}

// commandRuns は長いコマンドが d の間続く（画面もログも動かず、会話の記録も増えない）。この間は working と読み、何も送らない。
func commandRuns(t *testing.T, f *frozenScreen, r *receiver, d time.Duration) {
	t.Helper()
	for waited := time.Duration(0); waited < d; waited += 5 * time.Second {
		if got := f.see(5 * time.Second); !working(got) {
			t.Fatalf("長いコマンドの実行中（%s 後）に %s\n%s", waited, got, f.logText())
		}
	}
	if n := len(r.received()); n != 0 {
		t.Fatalf("作業中に送った: POST = %d 回\n%s", n, f.logText())
	}
}

// finishTurn はコマンドが終わり、最終報告を書き、最終の応答で番が終わるところまで進める。
func finishTurn(t *testing.T, f *frozenScreen) {
	t.Helper()
	f.env.Clock.Advance(2 * time.Second)
	f.probe.SetTurn(frozenPID, f.env.Clock.Now(), true) // コマンドの結果
	f.writeReportBody("learned for a long time\n")
	f.see(5 * time.Second)
	f.env.Clock.Advance(3 * time.Second)
	f.probe.SetTurn(frozenPID, f.env.Clock.Now(), false) // 最終の応答
	for range 3 {
		f.see(5 * time.Second)
	}
	f.settle()
}

// 受け入れ条件 1: 受理を確かめられず、モデルの呼び出しの行が 10 分以上出ない長いコマンドのあとに最終報告が書かれたら、pane.idle_report が 1 回だけ飛ぶ。
func TestLongCommandWithoutModelCallsPostsIdleReportOnce(t *testing.T) {
	r := newReceiver(t, "KEY-L-0001", "TOK-L-0001")
	f := newLongCommand(t, r)
	startLongCommand(t, f)

	commandRuns(t, f, r, 4*time.Hour) // issue の B3（約 4 時間）と同じ長さ
	finishTurn(t, f)

	hits := r.received()
	if len(hits) != 1 {
		t.Fatalf("POST = %d 回, want 1\n%s", len(hits), f.logText())
	}
	if pl := payloadOf(t, hits[0]); pl.Event != webhook.EventPaneIdleReport || pl.PaneID != "%0" || pl.Pane != "T1" {
		t.Errorf("payload = %+v", pl)
	}
	for range 40 { // そのあと止まったままでも、重ねて送らない
		f.see(time.Minute)
	}
	f.drain()
	if n := len(r.received()); n != 1 {
		t.Fatalf("重ねて送った: POST = %d 回\n%s", n, f.logText())
	}
}

// restart は serve の再起動を再現する。Notifier と Registry を作り直し、見張り（Unaccepted）は台帳からだけ引き継ぐ。
func (f *frozenScreen) restart(t *testing.T, r *receiver) {
	t.Helper()
	f.cancel()
	f.wait()
	f.un = pane.NewUnaccepted()
	f.rig.un = f.un
	f.build(r.target(3))
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: f.env.Cfg, Tmux: f.env.Tmux, Host: f.env.Host, Adapters: f.env.Adapters, Notes: f.env.Notes,
		Clock: f.env.Clock, Unaccepted: f.un, Observer: f.n,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.reg = reg
}

// 受け入れ条件 2: 見張りの途中で serve が再起動しても、同じケースで 1 回だけ飛ぶ。
func TestLongCommandAcrossServerRestartPostsIdleReportOnce(t *testing.T) {
	r := newReceiver(t, "KEY-L-0002", "TOK-L-0002")
	f := newLongCommand(t, r)
	startLongCommand(t, f)
	commandRuns(t, f, r, 20*time.Minute)

	f.restart(t, r) // 見張り・回は台帳から読み戻す
	commandRuns(t, f, r, 3*time.Hour)
	finishTurn(t, f)

	hits := r.received()
	if len(hits) != 1 {
		t.Fatalf("POST = %d 回, want 1\n%s", len(hits), f.logText())
	}
	if pl := payloadOf(t, hits[0]); pl.Event != webhook.EventPaneIdleReport || pl.PaneID != "%0" {
		t.Errorf("payload = %+v", pl)
	}
}

// 再起動を 2 度はさんでも 1 回（台帳が引き継ぎ続ける）。
func TestLongCommandAcrossTwoRestartsPostsIdleReportOnce(t *testing.T) {
	r := newReceiver(t, "KEY-L-0003", "TOK-L-0003")
	f := newLongCommand(t, r)
	startLongCommand(t, f)
	for range 2 {
		commandRuns(t, f, r, time.Hour)
		f.restart(t, r)
	}
	commandRuns(t, f, r, time.Hour)
	finishTurn(t, f)
	if n := len(r.received()); n != 1 {
		t.Fatalf("POST = %d 回, want 1\n%s", n, f.logText())
	}
}

// 見張りの記録（Enter の時刻・pid）が台帳の watch に書かれる（ER §9）。
func TestLedgerRecordsWatchOfUnacceptedPane(t *testing.T) {
	r := newReceiver(t, "KEY-L-0004", "TOK-L-0004")
	f := newLongCommand(t, r)
	startLongCommand(t, f)
	commandRuns(t, f, r, 10*time.Minute)
	data, err := os.ReadFile(f.state)
	if err != nil {
		t.Fatal(err)
	}
	var led struct {
		Watch map[string]struct {
			Enter time.Time `json:"enter"`
			PID   int       `json:"pid"`
		} `json:"watch"`
	}
	if err := json.Unmarshal(data, &led); err != nil {
		t.Fatal(err)
	}
	w, ok := led.Watch["%0"]
	if !ok || w.Enter.IsZero() || w.PID != frozenPID {
		t.Fatalf("台帳に見張りが無い: %s", data)
	}
}

// 受け入れ条件 3: 受理を確かめられず、実際にも作業していない（会話の記録の最後が番の終わり・記録が無い）ペインは working にせず、何も送らない。
func TestIdleOmpWithUnacceptedMarkIsNotWorking(t *testing.T) {
	cases := map[string]func(f *frozenScreen){
		"会話の記録が無い": func(*frozenScreen) {},
		"番が終わっている": func(f *frozenScreen) {
			f.env.Clock.Advance(time.Second)
			f.probe.SetTurn(frozenPID, f.env.Clock.Now(), false)
		},
		"Enter より前の番の途中で止まった記録だけ": func(f *frozenScreen) {
			f.probe.SetTurn(frozenPID, f.env.Clock.Now().Add(-time.Hour), true)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReceiver(t, "KEY-L-0005", "TOK-L-0005")
			f := newLongCommand(t, r)
			f.see(time.Second)
			f.un.Mark("%0", f.env.Clock.Now())
			setup(f)
			f.writeReport() // 前の作業の最終報告が置かれたままでも
			for range 30 {
				if got := f.see(time.Minute); got != agent.StateIdle {
					t.Fatalf("作業していないのに %s", got)
				}
			}
			f.drain()
			if n := len(r.received()); n != 0 {
				t.Fatalf("作業していないペインで %d 回送った", n)
			}
		})
	}
}

// 長いコマンドが終わって番が終わったのに最終報告が書かれていなければ送らない。
func TestLongCommandWithoutReportPostsNothing(t *testing.T) {
	r := newReceiver(t, "KEY-L-0006", "TOK-L-0006")
	f := newLongCommand(t, r)
	startLongCommand(t, f)
	commandRuns(t, f, r, time.Hour)
	f.env.Clock.Advance(time.Second)
	f.probe.SetTurn(frozenPID, f.env.Clock.Now(), false)
	for range 20 {
		f.see(time.Minute)
	}
	f.drain()
	if n := len(r.received()); n != 0 {
		t.Fatalf("最終報告が無いのに %d 回送った", n)
	}
}
