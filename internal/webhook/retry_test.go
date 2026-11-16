package webhook

import (
	"bytes"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// idleWithReport は working → レポート → idle まで進める（送信は idle の観測で始まり、終わるまで待つ）。
func (h *harness) idleWithReport() {
	h.t.Helper()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.observe(agent.StateIdle)
}

// Issue #93: 送信失敗（5xx）は上限つきで再送し、間隔は倍々。尽きたらログに残し、その回は二度と送らない。
func TestRetriesUpToLimitThenLogsAndStops(t *testing.T) {
	h := newHarness(t)
	h.setStatus(http.StatusServiceUnavailable)
	h.start()
	h.idleWithReport()

	if got := len(h.received()); got != h.cfg.MaxAttempts {
		t.Fatalf("POST = %d 回, want %d（上限）", got, h.cfg.MaxAttempts)
	}
	if got, want := h.sleeps(), []time.Duration{30 * time.Second, 60 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Errorf("再送の間隔 = %v, want %v", got, want)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `"msg":"webhook を送れませんでした"`) || !strings.Contains(logs, `"level":"ERROR"`) {
		t.Errorf("送れなかったことがログにありません:\n%s", logs)
	}
	if !strings.Contains(logs, `"attempts":3`) || !strings.Contains(logs, `"reason":"http_status"`) || !strings.Contains(logs, `"status":503`) {
		t.Errorf("回数・理由・応答がログにありません:\n%s", logs)
	}
	// 諦めた回は、そのあと何度見ても、レポートを更新しても、再起動しても送り直さない（項目は消え、回は done）。
	h.report(2000)
	h.observe(agent.StateIdle)
	h.observe(agent.StateIdle)
	h.restart()
	h.observe(agent.StateIdle)
	h.report(3000)
	h.observe(agent.StateIdle)
	if got := len(h.received()); got != h.cfg.MaxAttempts {
		t.Fatalf("上限のあとに送り直しました: %d 回", got)
	}
	if len(h.n.outbox) != 0 {
		t.Errorf("諦めた項目が outbox に残っています: %d 件", len(h.n.outbox))
	}
}

// 途中で成功したらそこで止まる。同じ本文（同じ event_id）で再送する。
func TestRetrySucceedsAndKeepsSameBody(t *testing.T) {
	h := newHarness(t)
	h.setStatus(http.StatusBadGateway)
	h.start()
	h.observe(agent.StateWorking)
	h.report(1000)
	h.onSleep = func() { h.setStatus(http.StatusOK) } // 1 回目の失敗のあと、受け手が復旧する
	h.observe(agent.StateIdle)

	hits := h.received()
	if len(hits) < 2 || string(hits[0].body) != string(hits[len(hits)-1].body) {
		t.Fatalf("再送の本文が変わっています: %d 回", len(hits))
	}
	if last := hits[len(hits)-1]; last.header.Get(headerEventID) == "" {
		t.Error("event_id ヘッダーがありません")
	}
	if strings.Contains(h.logs.String(), `"level":"ERROR"`) {
		t.Errorf("成功したのに ERROR が出ています:\n%s", h.logs.String())
	}
	if !strings.Contains(h.logs.String(), "webhook を送りました") {
		t.Error("成功のログがありません")
	}
}

// 設定の誤り（404 など）は再送しても無駄なので 1 回で止め、ログに残す。
func TestClientErrorIsNotRetried(t *testing.T) {
	h := newHarness(t)
	h.setStatus(http.StatusNotFound)
	h.start()
	h.idleWithReport()
	if got := len(h.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1", got)
	}
	if !strings.Contains(h.logs.String(), `"status":404`) {
		t.Errorf("ログに応答がありません:\n%s", h.logs.String())
	}
}

// 受け手に届かない（接続できない）ときも、上限つきで再送し、ログに残す。
func TestUnreachableReceiverRetriesAndLogs(t *testing.T) {
	h := newHarness(t)
	h.srv.Close() // 接続を拒否される
	h.start()
	h.idleWithReport()
	if got := len(h.sleeps()); got != h.cfg.MaxAttempts-1 {
		t.Fatalf("再送の待ち = %d 回, want %d", got, h.cfg.MaxAttempts-1)
	}
	if !strings.Contains(h.logs.String(), `"reason":"connect"`) {
		t.Errorf("理由がログにありません:\n%s", h.logs.String())
	}
}

// 再送の待ちの間隔は MaxWebhookRetryInterval を超えない。
func TestRetryDelayIsCapped(t *testing.T) {
	h := newHarness(t)
	h.cfg.MaxAttempts, h.cfg.RetryInterval = 6, 40*time.Minute
	h.setStatus(http.StatusInternalServerError)
	h.start()
	h.idleWithReport()
	for _, d := range h.sleeps() {
		if d > config.MaxWebhookRetryInterval {
			t.Fatalf("間隔 %v が上限 %v を超えています", d, config.MaxWebhookRetryInterval)
		}
	}
}

// 終了（ctx の取り消し）で再送の待ちを止める。中断した項目は失敗にせず、再起動後に同じ本文で続きから送る。
func TestShutdownDuringRetryKeepsItemForNextStart(t *testing.T) {
	h := newHarness(t)
	h.setStatus(http.StatusServiceUnavailable)
	cancel := h.start()
	h.onSleep = cancel
	h.idleWithReport()
	if got := len(h.received()); got != 1 {
		t.Fatalf("中断のあとに送っています: %d 回", got)
	}
	if strings.Contains(h.logs.String(), `"level":"ERROR"`) {
		t.Errorf("中断を失敗として記録しています:\n%s", h.logs.String())
	}

	h.setStatus(http.StatusOK)
	h.restart() // 再起動。台帳の項目をここから送る
	hits := h.received()
	if len(hits) != 2 {
		t.Fatalf("再起動後に続きから送っていません: %d 回", len(hits))
	}
	if !bytes.Equal(hits[0].body, hits[1].body) || hits[0].header.Get(headerEventID) != hits[1].header.Get(headerEventID) {
		t.Error("再起動の前後で本文・event_id が変わっています")
	}
	if len(h.n.outbox) != 0 {
		t.Errorf("送れた項目が outbox に残っています: %d 件", len(h.n.outbox))
	}
}

// Issue #93: 秘密の値（URL・署名の鍵・トークン）は、成功・503・接続不能・リダイレクト・中断（再送の待ちで終了。台帳に件が残る）の
// どの経路でも、ログにも台帳にも出ない。
func TestSecretsNeverAppearInLogsOrLedger(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"成功":     func(*harness) {},
		"503":    func(h *harness) { h.setStatus(http.StatusServiceUnavailable) },
		"接続不能":   func(h *harness) { h.srv.Close() },
		"リダイレクト": func(h *harness) { h.setStatus(http.StatusFound) },
		"中断": func(h *harness) {
			h.setStatus(http.StatusServiceUnavailable)
			h.onSleep = func() { h.cancel() } // 1 回目の失敗のあとの再送の待ちで終了する
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			setup(h)
			h.start()
			h.idleWithReport()
			ledger, err := os.ReadFile(h.state)
			if err != nil {
				t.Fatal(err)
			}
			out := h.logs.String() + "\n" + string(ledger)
			for _, secret := range []string{secretToken, secretKey, secretPath, strings.TrimPrefix(h.srv.URL, "http://")} {
				if strings.Contains(out, secret) {
					t.Errorf("%q が出ています:\n%s", secret, out)
				}
			}
			if out == "" {
				t.Fatal("ログも台帳も空です")
			}
		})
	}
}

// リダイレクトは追わない（Authorization・署名を別の宛先へ送らない）。再送もしない。
func TestRedirectIsNotFollowed(t *testing.T) {
	h := newHarness(t)
	h.setStatus(http.StatusTemporaryRedirect)
	h.start()
	h.idleWithReport()
	if got := len(h.received()); got != 1 {
		t.Fatalf("POST = %d 回, want 1", got)
	}
}
