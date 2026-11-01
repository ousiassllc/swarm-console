package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

// エージェントの設定: 変えた 1 キーだけが変わる。書かれているキーを空にはできない。
func TestSetupAgentSettings(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "config", "handwritten.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	writeFile(t, cfg, src, 0o600)
	p := startSetup(t, tm, home, cfg, "")
	p.listenKeep()
	p.skipToAgentsFrom("sessions（カンマ区切り）")
	p.keys("y") // はい
	p.text("omp の command")
	p.keys("End")
	p.typeText(" --slow")
	p.keys("Enter", "Enter", "Enter") // exit_command・auth_file は空のまま
	p.text("claude の command")
	p.keys("Enter")
	p.text("claude の exit_command")
	p.keys("C-u", "Enter") // 書かれているキーは空にできない
	p.text("空にはできません")
	p.typeText("/exit")
	p.keys("Enter", "Enter")
	p.text("codex の command")
	p.keys("Enter", "Enter", "Enter")
	p.text("トークンを発行しますか")
	p.keys("Enter")
	p.finalText("systemctl --user restart swarm-console")
	if code := p.exitCode(); code != 0 {
		t.Fatalf("終了コード = %d", code)
	}
	want := strings.Replace(string(src), `command = "omp --fast"`, `command = "omp --fast --slow"`, 1)
	if got := readFile(t, cfg); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// コメント入りの複数行配列を変えるときは確認する。了承しなければそのキーは元のまま。
func TestSetupCommentLossConfirm(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "config", "handwritten.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	writeFile(t, cfg, src, 0o600)
	p := startSetup(t, tm, home, cfg, "")
	p.listenKeep()
	p.text("sessions（カンマ区切り）")
	p.keys("Enter")
	p.text("allowed_roots（1 行 1 パス）")
	p.keys("C-Home", "C-k", "Enter") // 先頭行を消す
	p.text("denied_paths（1 行 1 パス）")
	p.keys("Enter")
	p.text("設定を変えますか")
	p.keys("Enter")
	p.text("トークンを発行しますか")
	p.keys("Enter")
	p.text("allowed_roots の配列の中のコメントが消えます")
	p.keys("Enter") // 既定は「元のままにする」
	p.finalText("変更はありません")
	if code := p.exitCode(); code != 0 {
		t.Fatalf("終了コード = %d", code)
	}
	if got := readFile(t, cfg); got != string(src) {
		t.Fatalf("了承なしで書き換えました:\n%s", got)
	}
}

// config.toml がシンボリックリンクなら、画面を開かず何も書かずに終わる（リンクが普通のファイルにならない）。
func TestSetupSymlinkRefused(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	const body = "listen_addr = \"127.0.0.1:1\"\n"
	target := filepath.Join(home, "elsewhere.toml")
	if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	if err := os.Symlink(target, cfg); err != nil {
		t.Fatal(err)
	}
	p := startSetup(t, tm, home, cfg, "")
	if code := p.exitCode(); code != 1 {
		t.Fatalf("終了コード = %d", code)
	}
	if got := p.stderr(); !strings.Contains(got, "シンボリックリンク") {
		t.Fatalf("標準エラー: %q", got)
	}
	if fi, err := os.Lstat(cfg); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("リンクが変わりました: %v %v", fi, err)
	}
	if got := readFile(t, target); got != body {
		t.Fatalf("リンク先が変わりました: %q", got)
	}
}
