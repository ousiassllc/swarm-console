// Package setup は `swarm-console setup` の画面を実 tmux の専用ソケットのペインで確かめる（NFR-TEST-13）。
// ペインで実バイナリを起動し、send-keys で入力し、capture-pane で画面を待つ。待ちは上限つきの条件待ち。
package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

const buildTimeout = 10 * time.Minute

var (
	binOnce sync.Once
	binPath string
	binRoot string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binRoot != "" {
		_ = os.RemoveAll(binRoot)
	}
	os.Exit(code)
}

func repoRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("go.mod が見つかりません")
		}
		dir = parent
	}
}

// bin は swarm-console の実バイナリ（テスト全体で 1 回だけビルドする）。
func bin(tb testing.TB) string {
	tb.Helper()
	binOnce.Do(func() {
		root, err := os.MkdirTemp("", "sc-setup-bin-")
		if err != nil {
			tb.Fatal(err)
		}
		binRoot = root
		exe := filepath.Join(root, "swarm-console")
		ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", exe, "./cmd/swarm-console") //nolint:gosec // 固定の引数
		cmd.Dir = repoRoot(tb)
		if out, err := cmd.CombinedOutput(); err != nil {
			tb.Fatalf("swarm-console をビルドできません: %v\n%s", err, out)
		}
		binPath = exe
	})
	if binPath == "" {
		tb.Fatal("swarm-console をビルドできていません")
	}
	return binPath
}

// pane は専用 tmux のペインで動く `swarm-console setup`。
type pane struct {
	tb       testing.TB
	tm       *tmuxtest.Server
	id       string
	exitFile string
	errFile  string // 標準エラーの出力先（画面の折り返しに左右されないよう、ペインではなくファイルで読む）
}

// startSetup は home を HOME にして `swarm-console setup --config cfg` をペインで起動する。
// term は TERM の値（"" なら xterm-256color）。終了コードは exitFile に書かれ、ペインは終了後も残る（remain-on-exit）。
func startSetup(tb testing.TB, tm *tmuxtest.Server, home, cfg, term string) *pane {
	tb.Helper()
	if term == "" {
		term = "xterm-256color"
	}
	dir := tb.TempDir()
	exitFile := filepath.Join(dir, "exit")
	errFile := filepath.Join(dir, "stderr")
	script := filepath.Join(dir, "run.sh")
	body := "#!/bin/sh\nexport HOME=" + shq(home) + " TERM=" + shq(term) + "\n" +
		shq(bin(tb)) + " setup --config " + shq(cfg) + " 2>" + shq(errFile) + "\necho $? > " + shq(exitFile) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // 実行するテスト用スクリプト
		tb.Fatal(err)
	}
	out := tm.Run("start-server", ";", "set", "-g", "remain-on-exit", "on", ";",
		"new-session", "-d", "-P", "-F", "#{pane_id}", "-s", "setup", "-x", "200", "-y", "50", script)
	return &pane{tb: tb, tm: tm, id: strings.TrimSpace(out), exitFile: exitFile, errFile: errFile}
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// text は画面に s が現れるまで待つ。
func (p *pane) text(s string) string {
	p.tb.Helper()
	return p.tm.WaitForText(p.id, s)
}

// finalText は setup が終わるのを待ってから、終了後の出力が現れるまで待つ。終了時の画面の消去で出力がスクロールバックへ移ることがあるので、履歴も見る。
func (p *pane) finalText(s string) string {
	p.tb.Helper()
	p.exitCode()
	var screen string
	tmuxtest.Eventually(p.tb, "終了後の出力: "+s, func() bool {
		screen = p.tm.Run("capture-pane", "-p", "-S", "-", "-t", p.id)
		return strings.Contains(screen, s)
	})
	return screen
}

// anyText は画面に candidates のどれかが現れるまで待ち、現れた画面を返す。
func (p *pane) anyText(candidates ...string) string {
	p.tb.Helper()
	return p.tm.WaitFor(p.id, func(screen string) bool {
		for _, c := range candidates {
			if strings.Contains(screen, c) {
				return true
			}
		}
		return false
	})
}

// type_ は文字をそのまま打つ。
func (p *pane) typeText(s string) {
	p.tb.Helper()
	p.tm.Run("send-keys", "-t", p.id, "-l", s)
}

// keys は tmux のキー名（Enter・Escape・C-c・End など）を送る。
func (p *pane) keys(names ...string) {
	p.tb.Helper()
	p.tm.Run(append([]string{"send-keys", "-t", p.id}, names...)...)
}

// exitCode は setup が終わるまで待って、終了コードを返す。
func (p *pane) exitCode() int {
	p.tb.Helper()
	var code int
	tmuxtest.Eventually(p.tb, "setup の終了", func() bool {
		b, err := os.ReadFile(p.exitFile)
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		code = n
		return err == nil
	})
	return code
}

// stderr は終了後の標準エラー。
func (p *pane) stderr() string {
	p.tb.Helper()
	b, err := os.ReadFile(p.errFile)
	if err != nil {
		p.tb.Fatal(err)
	}
	return string(b)
}

// listen は listen_addr の画面を、候補があるか（選択）ないか（手入力）に関わらず手入力の欄まで進める。
// 手入力の欄では、先頭の値（現在値）が入ったまま。
func (p *pane) listenManual() {
	p.tb.Helper()
	screen := p.anyText("listen_addr（IP:ポート）", "手で入力（ループバックなど）")
	if strings.Contains(screen, "手で入力（ループバックなど）") {
		p.keys("End", "Enter")
		p.text("listen_addr（IP:ポート）")
	}
}

// listenKeep は listen_addr の画面を、現在値のまま次へ進める（選択でも手入力でも Enter 1 回）。
func (p *pane) listenKeep() {
	p.tb.Helper()
	p.anyText("listen_addr（IP:ポート）", "手で入力（ループバックなど）")
	p.keys("Enter")
}

// skipToAgents は sessions → allowed_roots → denied_paths を何も変えずに通り、エージェントの確認で止まる。
func (p *pane) skipToAgents() {
	p.tb.Helper()
	p.text("sessions（カンマ区切り）")
	p.keys("Enter")
	p.text("allowed_roots（1 行 1 パス）")
	p.keys("Enter")
	p.text("denied_paths（1 行 1 パス）")
	p.keys("Enter")
	p.text("設定を変えますか")
}

// skipToAgentsFrom は label の画面（sessions）から、何も変えずにエージェントの確認まで進む。
func (p *pane) skipToAgentsFrom(label string) {
	p.tb.Helper()
	p.text(label)
	p.keys("Enter")
	p.text("allowed_roots（1 行 1 パス）")
	p.keys("Enter")
	p.text("denied_paths（1 行 1 パス）")
	p.keys("Enter")
	p.text("設定を変えますか")
}
