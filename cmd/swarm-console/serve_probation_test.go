package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const pendingFile = "swarm-console.update-pending"

func (g *serveRig) writeMarker(from, to string, boots int) {
	g.t.Helper()
	b, _ := json.Marshal(map[string]any{"from": from, "to": to, "boots": boots})
	g.writeFile(filepath.Join(g.binDir, pendingFile), string(b), 0o600)
}

func (g *serveRig) boots() int {
	g.t.Helper()
	var m struct {
		Boots int `json:"boots"`
	}
	if err := json.Unmarshal([]byte(g.read(pendingFile)), &m); err != nil {
		g.t.Fatalf("マーカーを読めません: %v", err)
	}
	return m.Boots
}

// 試運転：1 回目・2 回目の起動は boots を数えて起動し、待ち受けから 60 秒（clock）生きたらマーカーを消す（FR-UPD-17）。
func TestServeProbationConfirmsAfter60Seconds(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "update_check = false\n")
	g.writeFile(filepath.Join(g.binDir, "swarm-console.old"), oldBin, 0o755)
	g.writeMarker("v0.1.4", "v0.1.5", 1) // 2 回目の起動
	g.start()
	if got := g.boots(); got != 2 {
		t.Fatalf("boots = %d, want 2", got)
	}
	u := g.hostUpdate()
	if a := g.apply(u); a["state"] != "probation" || a["from"] != "v0.1.4" {
		t.Fatalf("試運転中の apply = %v", a)
	}
	g.untilWaiting(1)
	g.clk.Advance(59 * time.Second)
	if !g.exists(pendingFile) {
		t.Fatal("59 秒で確定しました")
	}
	g.clk.Advance(time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for g.exists(pendingFile) {
		if time.Now().After(deadline) {
			t.Fatal("60 秒生きてもマーカーが消えません")
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
	// 確定後は、update_check = false なので off（試運転中だけは設定によらず probation を見せる）。
	for g.apply(g.hostUpdate())["state"] != "off" {
		if time.Now().After(deadline) {
			t.Fatalf("確定後の apply が probation のままです")
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
	if g.read("swarm-console.old") != oldBin {
		t.Error(".old は確定のあとも 1 世代残す")
	}
	g.cancel()
	if code := g.wait(); code != 0 {
		t.Errorf("終了コード = %d", code)
	}
	if !strings.Contains(g.stderr.String(), "試運転が済みました") {
		t.Errorf("INFO ログがありません:\n%s", g.stderr.String())
	}
}

// 試運転中の apply は、update_auto が true なら probation（確定で idle）。
func TestServeProbationApplyStateWithAutoOn(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "")
	g.origin.HandleFunc("api.github.com", releaseJSON("v0.1.5"))
	g.writeMarker("v0.1.4", "v0.1.5", 0)
	g.writeFile(filepath.Join(g.binDir, "swarm-console.old"), oldBin, 0o755)
	g.start()
	if a := g.apply(g.hostUpdate()); a["state"] != "probation" || a["from"] != "v0.1.4" {
		t.Fatalf("apply = %v", a)
	}
	g.untilWaiting(2) // 確認の待ち（1 分）と試運転の待ち（60 秒）
	g.clk.Advance(time.Minute)
	deadline := time.Now().Add(15 * time.Second)
	for g.apply(g.hostUpdate())["state"] != "idle" {
		if time.Now().After(deadline) {
			t.Fatal("確定しても apply が idle になりません")
		}
		time.Sleep(10 * time.Millisecond) // 上限つきのポーリング間隔
	}
	g.cancel()
	g.wait()
}

// 試運転中に確認が update_available でも、入れ替えを始めない（通信しない・.old とマーカーを書かない・警告も出さない。FR-UPD-12）。
func TestServeProbationDoesNotStartApply(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "")
	env := updateEnv
	env.FirstDelay = 30 * time.Second // 確認（30 秒）を、試運転の確定（60 秒）より先にする
	useVersion(t, "v0.1.5", env)
	g.release("v0.1.6", newBin)
	g.writeFile(filepath.Join(g.binDir, "swarm-console.old"), oldBin, 0o755)
	g.writeMarker("v0.1.4", "v0.1.5", 0)
	g.start()
	g.untilWaiting(2) // 確認の待ち（30 秒）と試運転の待ち（60 秒）
	g.clk.Advance(30 * time.Second)
	u := g.untilUpdate("update_available")
	g.untilWaiting(2) // 確認の結果の処理（入れ替えの判断）が済み、次の周期の待ちに戻った
	if a := g.apply(u); a["state"] != "probation" || a["from"] != "v0.1.4" {
		t.Errorf("apply = %v, want probation（from v0.1.4）", a)
	}
	if a := g.apply(g.hostUpdate()); a["state"] != "probation" {
		t.Errorf("確認の処理のあとの apply = %v", a)
	}
	for _, r := range g.origin.Requests() {
		if r.Host != "api.github.com" {
			t.Errorf("確認以外の通信をしました: %+v", r)
		}
	}
	if g.read("swarm-console") != oldBin || g.read("swarm-console.old") != oldBin || g.boots() != 1 {
		t.Errorf("置き場が変わりました: bin=%q old=%q marker=%q", g.read("swarm-console"), g.read("swarm-console.old"), g.read(pendingFile))
	}
	if got := g.files(); len(got) != 3 {
		t.Errorf("置き場のファイル = %v", got)
	}
	select {
	case code := <-g.done:
		t.Fatalf("試運転中に serve が終了しました (exit %d)", code)
	default:
	}
	g.cancel()
	g.wait()
	for _, bad := range []string{"自己更新を始めます", "自己更新できませんでした", "自己更新を行えませんでした"} {
		if strings.Contains(g.stderr.String(), bad) {
			t.Errorf("ログに %q が出ています:\n%s", bad, g.stderr.String())
		}
	}
}

// 60 秒より前に終了（SIGTERM）すれば、マーカーは残る。
func TestServeProbationMarkerRemainsWhenStoppedEarly(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "update_check = false\n")
	g.writeMarker("v0.1.4", "v0.1.5", 0)
	g.start()
	g.untilWaiting(1)
	g.clk.Advance(30 * time.Second)
	g.cancel()
	g.wait()
	if got := g.boots(); got != 1 {
		t.Errorf("boots = %d, want 1", got)
	}
}

// 3 回目の起動：設定の読み込みより前に .old を戻し、.update-failed を書き、ERROR ログを出して終了コード 1。
// 設定が壊れていても戻せる（設定の読み込みより前に判定する）。
func TestServeProbationRollsBackOnThirdBootBeforeConfig(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "")
	g.writeFile(g.cfg, "これは TOML ではない = = \n", 0o600)
	g.writeFile(filepath.Join(g.binDir, "swarm-console.old"), oldBin, 0o755)
	g.writeFile(g.exe, newBin, 0o755)
	g.writeMarker("v0.1.4", "v0.1.5", 2)
	g.ctx, g.cancel = ctxCancel(t)
	code := run(g.ctx, []string{"serve", "--config", g.cfg}, &strings.Builder{}, g.stderr)
	if code != 1 {
		t.Fatalf("終了コード = %d\n%s", code, g.stderr.String())
	}
	if g.read("swarm-console") != oldBin || g.exists("swarm-console.old") || g.exists(pendingFile) {
		t.Errorf("戻っていません: bin=%q files=%v", g.read("swarm-console"), g.files())
	}
	var f struct{ Version, At string }
	if err := json.Unmarshal([]byte(g.read("swarm-console.update-failed")), &f); err != nil || f.Version != "v0.1.5" || f.At != clkStart.Format(time.RFC3339) {
		t.Errorf(".update-failed = %q", g.read("swarm-console.update-failed"))
	}
	if fi, _ := os.Stat(filepath.Join(g.binDir, "swarm-console.update-failed")); fi.Mode().Perm() != 0o600 {
		t.Errorf(".update-failed の権限 = %v", fi.Mode().Perm())
	}
	logs := g.stderr.String()
	if !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, `"from":"v0.1.4"`) || !strings.Contains(logs, `"to":"v0.1.5"`) {
		t.Errorf("ERROR ログ（from・to）がありません:\n%s", logs)
	}
	if strings.Contains(logs, "TOML") || strings.Contains(logs, "起動します") {
		t.Errorf("設定の読み込み・起動に進んでいます:\n%s", logs)
	}
}

// to が自分の版でないマーカーは消して通常どおり起動する。
func TestServeProbationMarkerForOtherVersionIsDeleted(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "update_check = false\n")
	g.writeMarker("v0.1.4", "v0.1.5", 2)
	g.writeFile(filepath.Join(g.binDir, "swarm-console.old"), oldBin, 0o755)
	g.start()
	if g.exists(pendingFile) {
		t.Error("マーカーが残っています")
	}
	if g.read("swarm-console") != oldBin || !g.exists("swarm-console.old") || g.exists("swarm-console.update-failed") {
		t.Errorf("置き場 = %v", g.files())
	}
	if a := g.apply(g.hostUpdate()); a["state"] != "off" {
		t.Errorf("apply = %v", a)
	}
	g.cancel()
	g.wait()
}

// .old が無いときは、マーカーを消して通常どおり起動を続け（ERROR ログ）、以後は数えない。
func TestServeProbationWithoutOldContinuesNormally(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "update_check = false\n")
	g.writeMarker("v0.1.4", "v0.1.5", 2)
	g.start()
	if g.exists(pendingFile) || g.exists("swarm-console.update-failed") {
		t.Errorf("置き場 = %v", g.files())
	}
	if !strings.Contains(g.stderr.String(), `"level":"ERROR"`) {
		t.Errorf("ERROR ログがありません:\n%s", g.stderr.String())
	}
	g.cancel()
	if code := g.wait(); code != 0 {
		t.Errorf("終了コード = %d", code)
	}
}

// bind 失敗（tailscale が未起動など）の起動も試運転の回数に数える：待ち受けに失敗して終了してもマーカーは残り、60 秒は経たない。
func TestServeProbationCountsBindFailure(t *testing.T) {
	g := newServeRig(t, "v0.1.5", "update_check = false\n")
	g.writeMarker("v0.1.4", "v0.1.5", 0)
	busy := listenOn(t, g.addr)
	defer func() { _ = busy.Close() }()
	g.ctx, g.cancel = ctxCancel(t)
	if code := run(g.ctx, []string{"serve", "--config", g.cfg}, &strings.Builder{}, g.stderr); code != 1 {
		t.Fatalf("終了コード = %d\n%s", code, g.stderr.String())
	}
	if got := g.boots(); got != 1 {
		t.Errorf("boots = %d, want 1", got)
	}
}
