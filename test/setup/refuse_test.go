package setup

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

// 端末でないとき: 何も読まず何も書かず、理由と手書きの案内を標準エラーへ、終了コード 1。
func TestSetupNonTTY(t *testing.T) {
	home := newHome(t)
	dir := filepath.Join(home, "conf")
	cfg := filepath.Join(dir, "config.toml")
	cmd := exec.CommandContext(context.Background(), bin(t), "setup", "--config", cfg) //nolint:gosec // テストがビルドしたバイナリ
	cmd.Env = append(os.Environ(), "HOME="+home, "TERM=xterm-256color")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr // 標準入力は /dev/null
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError) //nolint:errorlint // 直接の型で十分
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("終了コード 1 のはず: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("標準出力: %q", stdout.String())
	}
	for _, want := range []string{"端末ではありません", "listen_addr", "手で書く"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("標準エラーに %q がありません:\n%s", want, stderr.String())
		}
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("何も書かないはずがフォルダが作られました")
	}
}

// TERM=dumb は端末でも対話しない（終了コード 1・何も書かない）。
func TestSetupTermDumb(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	cfg := filepath.Join(home, "conf", "config.toml")
	p := startSetup(t, tm, home, cfg, "dumb")
	if code := p.exitCode(); code != 1 {
		t.Fatalf("終了コード = %d", code)
	}
	if got := p.stderr(); !strings.Contains(got, "TERM=dumb") || !strings.Contains(got, "listen_addr") {
		t.Fatalf("標準エラー: %q", got)
	}
	if _, err := os.Stat(filepath.Dir(cfg)); err == nil {
		t.Fatal("何も書かないはずがフォルダが作られました")
	}
}

// 構文エラー・未知のキーは、画面を開かず行番号・キー名つきで終わる。ファイルはそのまま。
func TestSetupBrokenFileDoesNotOpen(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"未知のキー", "listen_addr = \"127.0.0.1:1\"\nmystery = 1\n", "mystery（2 行目）"},
		{"構文エラー", "listen_addr = \"127.0.0.1:1\"\nsessions = [\n", "TOML を読めません"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tmuxtest.New(t)
			home := newHome(t)
			cfg := filepath.Join(home, "config.toml")
			if err := os.WriteFile(cfg, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			p := startSetup(t, tm, home, cfg, "")
			if code := p.exitCode(); code != 1 {
				t.Fatalf("終了コード = %d", code)
			}
			if got := p.stderr(); !strings.Contains(got, tt.want) {
				t.Fatalf("標準エラーに %q がありません: %q", tt.want, got)
			}
			if strings.Contains(p.tm.Capture(p.id), "listen_addr（IP:ポート）") {
				t.Error("画面を開いています")
			}
			if got := readFile(t, cfg); got != tt.body {
				t.Fatalf("ファイルが変わりました: %q", got)
			}
		})
	}
}

// 値が不正（listen_addr 未設定）なファイルは開いて直させる。入力中に serve と同じ検証でその場でエラーになり、通るまで進めない。
func TestSetupFixesInvalidFile(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	cfg := filepath.Join(home, "config.toml")
	const src = "# 手書き\nsessions = [\"0\"]\n"
	if err := os.WriteFile(cfg, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	p := startSetup(t, tm, home, cfg, "")
	p.text("現在の config.toml の値が不正です")
	p.keys("Enter")
	p.listenManual()
	p.typeText("0.0.0.0:48120")
	p.keys("Enter")
	p.text("0.0.0.0・:: は使えません") // その場でエラー。次へは進まない
	p.keys("C-u")
	p.typeText("127.0.0.1:48120")
	p.keys("Enter")
	p.skipToAgents()
	p.keys("Enter")
	p.text("トークンを発行しますか")
	p.keys("Enter")
	p.finalText("systemctl --user restart swarm-console")
	if code := p.exitCode(); code != 0 {
		t.Fatalf("終了コード = %d", code)
	}
	if got, want := readFile(t, cfg), src+"listen_addr = \"127.0.0.1:48120\"\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
