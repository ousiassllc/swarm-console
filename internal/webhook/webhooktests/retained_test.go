package webhooktests

import (
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// Issue #115（P-107）: 画面が変わらず、ログにも作業の行が 10 分以上出ない作業（長い CI 待ちなど）のあとで作業が続き、
// 最終報告が書き直されたら、2 件目が（少し遅れて）1 回だけ飛ぶ。書き直しが無ければ送らない。

// newRetainingScreen は本番の配線（Notifier と Registry が同じ Unaccepted を持つ）の frozenScreen。
func newRetainingScreen(t *testing.T, r *receiver) *frozenScreen {
	t.Helper()
	return newFrozenScreenWith(t, r, pane.NewUnaccepted(), true)
}

// working は作業中と読めたこと（画面が 15 分変わらないと、ログで読んだ working は stalled になる。回では同じ扱い）。
func working(s agent.State) bool { return s == agent.StateWorking || s == agent.StateStalled }

// firstPost は起動画面のまま作業した omp が、最終報告を書いて 1 件目を送るところまで進める。
func firstPost(t *testing.T, f *frozenScreen, r *receiver) {
	t.Helper()
	f.see(time.Second)
	f.un.Mark("%0", f.env.Clock.Now()) // dispatch が Enter の受理を確かめられなかった
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	if got := f.see(5 * time.Second); !working(got) {
		t.Fatalf("起動画面のまま作業しているのに %s", got)
	}
	f.writeReportBody("first\n")
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	f.see(5 * time.Second)
	if got := f.see(3 * time.Minute); got != agent.StateIdle {
		t.Fatalf("作業の跡が途切れたのに %s", got)
	}
	f.see(5 * time.Second)
	f.settle()
	if n := len(r.received()); n != 1 {
		t.Fatalf("1 件目: POST = %d 回, want 1\n%s", n, f.logText())
	}
}

// longWait は画面もログも動かないまま d 待つ（スナップショットは 5 秒おき）。
func longWait(t *testing.T, f *frozenScreen, d time.Duration) {
	t.Helper()
	for waited := time.Duration(0); waited < d; waited += 5 * time.Second {
		if got := f.see(5 * time.Second); got != agent.StateIdle {
			t.Fatalf("画面もログも止まっているのに %s", got)
		}
	}
}

// 受け入れ条件 1・4: 画面もログも 10 分より長く止まったまま待ち、作業が続いて最終報告が書き直されたら、2 件目が 1 回だけ飛ぶ。
func TestFrozenScreenSilentOverTenMinutesThenRewritePostsSecondOnce(t *testing.T) {
	r := newReceiver(t, "KEY-R-0001", "TOK-R-0001")
	f := newRetainingScreen(t, r)
	firstPost(t, f, r)

	longWait(t, f, 25*time.Minute) // 10 分の上限を越えて待つ（長い CI 待ち）。この間は何も送らない
	if n := len(r.received()); n != 1 {
		t.Fatalf("待っている間に送った: POST = %d 回\n%s", n, f.logText())
	}

	// 作業が戻る。最終報告を書き直し、ログの作業の跡が途切れて idle になる。
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	var got agent.State
	for range 8 { // ログは 30 秒おきに読むので、working と読むまで少し遅れる
		if got = f.see(5 * time.Second); working(got) {
			break
		}
	}
	if !working(got) {
		t.Fatalf("10 分以上たって作業が戻ったのに %s", got)
	}
	f.writeReportBody("first, then rewritten after the long wait\n")
	f.probe.AddWork(frozenPID, f.env.Clock.Now())
	f.see(30 * time.Second)
	if got := f.see(3 * time.Minute); got != agent.StateIdle {
		t.Fatalf("作業の跡が途切れたのに %s", got)
	}
	for range 5 {
		f.see(5 * time.Second)
	}
	f.settle()
	hits := r.received()
	if len(hits) != 2 {
		t.Fatalf("POST = %d 回, want 2\n%s", len(hits), f.logText())
	}
	a, b := payloadOf(t, hits[0]), payloadOf(t, hits[1])
	if b.Event != webhook.EventPaneIdleReport || a.EventID == b.EventID {
		t.Errorf("2 件目 = %+v（1 件目の event_id %s）", b, a.EventID)
	}

	// 2 件目のあとも同じ idle では重ねて送らず、また長く待ってもそのまま。
	longWait(t, f, 15*time.Minute)
	f.drain()
	if n := len(r.received()); n != 2 {
		t.Fatalf("2 件目のあとに重ねて送った: POST = %d 回, want 2\n%s", n, f.logText())
	}
}

// 受け入れ条件 2: 10 分より長く止まったあとに作業が戻っても、最終報告を書き直していなければ送らない。
func TestFrozenScreenSilentOverTenMinutesWithoutRewritePostsNoSecond(t *testing.T) {
	r := newReceiver(t, "KEY-R-0002", "TOK-R-0002")
	f := newRetainingScreen(t, r)
	firstPost(t, f, r)

	for range 2 { // 長く待っては作業が戻ることを繰り返す。最終報告はそのまま
		longWait(t, f, 15*time.Minute)
		f.probe.AddWork(frozenPID, f.env.Clock.Now())
		var got agent.State
		for range 8 {
			if got = f.see(5 * time.Second); working(got) {
				break
			}
		}
		if !working(got) {
			t.Fatalf("作業が戻ったのに %s", got)
		}
		f.see(3 * time.Minute)
	}
	f.drain()
	if n := len(r.received()); n != 1 {
		t.Fatalf("書き直していないのに送りました: POST = %d 回, want 1\n%s", n, f.logText())
	}
}
