package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// update_available ＋ update_auto（既定 true）：確認 → ダウンロード → 検証 → 入れ替え → 正常終了（終了コード 0）まで自動（FR-UPD-7・11・16）。
func TestServeSelfUpdateReplacesAndExitsZero(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "")
	g.release("v0.1.5", newBin)
	g.start()
	u := g.hostUpdate()
	if u["status"] != "pending" || u["current"] != "v0.1.4" || g.apply(u)["state"] != "idle" {
		t.Fatalf("起動直後の update = %v", u)
	}
	if n := len(g.origin.Requests()); n != 0 {
		t.Fatalf("1 分前に通信しました（%d 件）", n)
	}
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	if code := g.wait(); code != 0 {
		t.Fatalf("終了コード = %d\n%s", code, g.stderr.String())
	}
	if g.read("swarm-console") != newBin || g.read("swarm-console.old") != oldBin {
		t.Errorf("入れ替わっていない: bin=%q old=%q", g.read("swarm-console"), g.read("swarm-console.old"))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(g.read("swarm-console.update-pending")), &m); err != nil ||
		m["from"] != "v0.1.4" || m["to"] != "v0.1.5" || m["boots"] != float64(0) {
		t.Errorf("マーカー = %q (%v)", g.read("swarm-console.update-pending"), err)
	}
	if got := g.files(); !slices.Equal(got, []string{"swarm-console", "swarm-console.old", "swarm-console.update-pending"}) {
		t.Errorf("置き場のファイル = %v", got)
	}
	logs := g.stderr.String()
	for _, want := range []string{`"msg":"新しい版があります"`, `"msg":"自己更新を始めます"`, `"msg":"終了シグナルを受けました。進行中の操作を待ちます"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("ログに %s がありません:\n%s", want, logs)
		}
	}
}

// update_auto = false：確認だけ。host_status は update_available・apply off で、バイナリは変わらず、serve は動き続ける。
func TestServeUpdateAutoFalseOnlyChecks(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "update_auto = false\n")
	g.release("v0.1.5", newBin)
	g.start()
	if a := g.apply(g.hostUpdate()); a["state"] != "off" {
		t.Fatalf("起動直後の apply = %v", a)
	}
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	u := g.untilUpdate("update_available")
	if u["latest"] != "v0.1.5" || u["checked_at"] == nil || g.apply(u)["state"] != "off" {
		t.Errorf("update = %v", u)
	}
	for _, r := range g.origin.Requests() {
		if r.Host != "api.github.com" {
			t.Errorf("確認以外の通信: %s%s", r.Host, r.Path)
		}
	}
	if g.read("swarm-console") != oldBin || len(g.files()) != 1 {
		t.Errorf("置き場が変わりました: %v", g.files())
	}
	g.cancel()
	if code := g.wait(); code != 0 {
		t.Errorf("終了コード = %d", code)
	}
}

// update_check = false：確認も入れ替えもしない（update_auto に関わらず）。host_status は disabled・off。
func TestServeUpdateCheckFalseDoesNothing(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "update_check = false\nupdate_auto = true\n")
	g.release("v0.1.5", newBin)
	g.start()
	u := g.hostUpdate()
	if u["status"] != "disabled" || u["current"] != "v0.1.4" || g.apply(u)["state"] != "off" || u["checked_at"] != nil {
		t.Fatalf("update = %v", u)
	}
	if w := g.clk.Waiters(); w != 0 {
		t.Errorf("確認の待ちがあります（%d）", w)
	}
	g.clk.Advance(48 * time.Hour)
	if n := len(g.origin.Requests()); n != 0 {
		t.Errorf("update_check = false なのに通信しました（%d 件）", n)
	}
	g.cancel()
	g.wait()
}

// update_check_interval ごとに再び確認する（設定が効く）。
func TestServeChecksEveryConfiguredInterval(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "update_check_interval = \"2h\"\n")
	g.release("v0.1.5", newBin)
	g.start()
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	g.untilUpdate("up_to_date")
	g.untilWaiting(1)
	g.clk.Advance(2*time.Hour - time.Second)
	if n := len(g.origin.Requests()); n != 1 {
		t.Fatalf("2 時間の前に通信しました（%d 件）", n)
	}
	g.clk.Advance(time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for len(g.origin.Requests()) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("2 時間後に再確認しません")
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
	g.cancel()
	g.wait()
}

// GitHub が応答しなくても、serve は応答し続け、SIGTERM（ctx のキャンセル）で 10 秒以内に終わる（FR-UPD-8・NFR-AVL-9）。
func TestServeSurvivesHangingGitHubAndStopsPromptly(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "")
	started := make(chan struct{})
	g.origin.HandleFunc("api.github.com", func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	g.start()
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	<-started
	if u := g.hostUpdate(); u["status"] != "pending" {
		t.Errorf("確認中の update = %v", u)
	}
	begin := time.Now()
	g.cancel()
	if code := g.wait(); code != 0 {
		t.Errorf("終了コード = %d\n%s", code, g.stderr.String())
	}
	if d := time.Since(begin); d > 10*time.Second {
		t.Errorf("終了に %s かかりました", d)
	}
}

// 確認がエラーでも serve は止まらず、host_status は reason を返す（URL・応答本文は出ない）。
func TestServeReportsUnknownReasonWithoutLeaking(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "")
	g.origin.HandleFunc("api.github.com", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "SECRET-BODY https://evil.example/x", http.StatusForbidden)
	})
	g.start()
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	u := g.untilUpdate("unknown")
	if u["reason"] != "rate_limit" {
		t.Errorf("update = %v", u)
	}
	b, _ := json.Marshal(u)
	logs := g.stderr.String()
	for _, leak := range []string{"SECRET-BODY", "evil.example"} {
		if strings.Contains(string(b), leak) || strings.Contains(logs, leak) {
			t.Errorf("%q が応答かログに出ています", leak)
		}
	}
	g.cancel()
	g.wait()
}

// 入れ替えに失敗（404）しても serve は動き続け、apply は failed（reason 付き）。置き場は元のまま一時ファイルも無い。
func TestServeApplyFailureKeepsServing(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "")
	g.origin.HandleFunc("api.github.com", releaseJSON("v0.1.5"))
	g.origin.HandleFunc("github.com", http.NotFound)
	g.start()
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	deadline := time.Now().Add(15 * time.Second)
	var a map[string]any
	for {
		a = g.apply(g.hostUpdate())
		if a["state"] == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("apply が failed になりません: %v", a)
		}
		time.Sleep(10 * time.Millisecond) // 上限つきのポーリング間隔
	}
	if a["reason"] != "asset_missing" || a["at"] == nil {
		t.Errorf("apply = %v", a)
	}
	if u := g.hostUpdate(); u["status"] != "update_available" || u["latest"] != "v0.1.5" {
		t.Errorf("update = %v", u)
	}
	if g.read("swarm-console") != oldBin || len(g.files()) != 1 {
		t.Errorf("置き場 = %v", g.files())
	}
	select {
	case code := <-g.done:
		t.Fatalf("入れ替えに失敗したのに serve が終了しました (exit %d)", code)
	default:
	}
	g.cancel()
	g.wait()
}

// ダウンロードの途中で SIGTERM（ctx のキャンセル）が来たら、通信を中断し、.new を消し、本体は元のままで終了する（FR-UPD-8・15）。
func TestServeSigtermDuringDownloadCleansUp(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "")
	g.release("v0.1.5", newBin)
	partial := make(chan struct{})
	g.origin.HandleFunc("github.com", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			_, _ = w.Write([]byte(sha(newBin) + "  swarm-console_v0.1.5_linux_amd64\n"))
			return
		}
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		close(partial)
		<-r.Context().Done()
	})
	g.start()
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)
	<-partial
	deadline := time.Now().Add(15 * time.Second)
	for !g.exists("swarm-console.new") {
		if time.Now().After(deadline) {
			t.Fatal(".new が作られません")
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
	begin := time.Now()
	g.cancel()
	if code := g.wait(); code != 0 {
		t.Errorf("終了コード = %d", code)
	}
	if d := time.Since(begin); d > 10*time.Second {
		t.Errorf("終了に %s かかりました", d)
	}
	if g.read("swarm-console") != oldBin || len(g.files()) != 1 {
		t.Errorf("置き場 = %v", g.files())
	}
}
