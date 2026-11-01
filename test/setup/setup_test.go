package setup

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

func newHome(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // テストが作った一時フォルダ
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 新規作成 → トークン発行 → そのまま serve が起動できる。書くのは変えたキー（listen_addr）だけ。
func TestSetupNewFileThenServe(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	cfg := filepath.Join(home, ".config", "swarm-console", "config.toml")
	addr := freeAddr(t)

	p := startSetup(t, tm, home, cfg, "")
	p.listenManual()
	p.typeText(addr)
	p.keys("Enter")
	p.skipToAgents()
	p.keys("Enter") // いいえ（エージェントは変えない）
	p.text("トークンを発行しますか")
	p.keys("y")
	p.text("トークンの名前")
	p.typeText("dev")
	p.keys("Enter")
	p.text("窓口の範囲")
	p.keys("Enter") // ops
	screen := p.finalText("systemctl --user restart swarm-console")
	if code := p.exitCode(); code != 0 {
		t.Fatalf("終了コード = %d\n%s", code, screen)
	}
	for _, want := range []string{"保存しました: " + cfg, "- listen_addr", "sc_"} {
		if !strings.Contains(screen, want) {
			t.Errorf("画面に %q がありません:\n%s", want, screen)
		}
	}

	fi, err := os.Stat(cfg)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("新規は権限 600: %v %v", fi, err)
	}
	var doc map[string]any
	if err := toml.Unmarshal([]byte(readFile(t, cfg)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 1 || doc["listen_addr"] != addr {
		t.Fatalf("書いたのは listen_addr だけのはず（既定値を書き出さない）: %v", doc)
	}
	tokens := readFile(t, filepath.Join(filepath.Dir(cfg), "tokens.toml"))
	if !strings.Contains(tokens, `name = 'dev'`) && !strings.Contains(tokens, `name = "dev"`) {
		t.Errorf("tokens.toml に dev がありません:\n%s", tokens)
	}
	if strings.Contains(tokens, "sc_") {
		t.Error("tokens.toml に平文があります")
	}

	// 手で足すキー（聞かないキー）と共存して、そのまま serve が起動できる。
	extra := "\ntmux_socket = \"" + tm.Socket + "\"\nreport_dir = \"" + filepath.Join(home, "reports") + "\"\ndisk_mounts = [\"/\"]\n"
	appendFile(t, cfg, extra)
	serveStarts(t, home, cfg, addr)
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// serveStarts は実バイナリの serve を起動し、待ち受けが始まり、SIGTERM で正常に終わることを確かめる。
func serveStarts(t *testing.T, home, cfg, addr string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin(t), "serve", "--config", cfg) //nolint:gosec // テストがビルドしたバイナリ
	cmd.Env = append(os.Environ(), "HOME="+home)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tmuxtest.Eventually(t, "serve の待ち受け", func() bool {
		select {
		case err := <-done:
			t.Fatalf("serve が先に終了しました: %v\n%s", err, stderr.String())
		default:
		}
		c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	})
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve が正常に終了しません: %v\n%s", err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serve が SIGTERM で終わりません")
	}
}

// 手書きの config.toml を開いて 1 項目だけ変えても、変えた値の範囲の外は 1 バイトも変わらない。
func TestSetupHandwrittenOneChange(t *testing.T) {
	tm := tmuxtest.New(t)
	home := newHome(t)
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "config", "handwritten.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	writeFile(t, cfg, src, 0o400)

	p := startSetup(t, tm, home, cfg, "")
	p.listenKeep()
	p.text("sessions（カンマ区切り）")
	p.keys("End")
	p.typeText(", work")
	p.keys("Enter")
	p.text("default_session")
	p.keys("Enter") // 先頭（0）のまま
	p.text("allowed_roots（1 行 1 パス）")
	p.keys("Enter")
	p.text("denied_paths（1 行 1 パス）")
	p.keys("Enter")
	p.text("設定を変えますか")
	p.keys("Enter")
	p.text("トークンを発行しますか")
	p.keys("Enter") // 発行しない
	screen := p.finalText("systemctl --user restart swarm-console")
	if code := p.exitCode(); code != 0 {
		t.Fatalf("終了コード = %d\n%s", code, screen)
	}
	want := strings.Replace(string(src), `sessions    = ["0"]`, `sessions    = ["0", "work"]`, 1)
	if got := readFile(t, cfg); got != want {
		t.Fatalf("変えた値の範囲の外が変わっています\n got:\n%s\nwant:\n%s", got, want)
	}
	if fi, _ := os.Stat(cfg); fi.Mode().Perm() != 0o400 {
		t.Errorf("既存ファイルの権限は変えない: %v", fi.Mode().Perm())
	}
	if !strings.Contains(screen, "- sessions") || strings.Contains(screen, "- denied_paths") {
		t.Errorf("変えたキーの一覧が違います:\n%s", screen)
	}
}

// 中止（Esc・Ctrl+C）では何も書かず、終了コード 1。
func TestSetupAbortWritesNothing(t *testing.T) {
	for _, key := range []string{"Escape", "C-c"} {
		t.Run(key, func(t *testing.T) {
			tm := tmuxtest.New(t)
			home := newHome(t)
			dir := filepath.Join(home, "conf")
			cfg := filepath.Join(dir, "config.toml")
			p := startSetup(t, tm, home, cfg, "")
			p.listenManual()
			p.typeText("127.0.0.1:1")
			p.keys(key)
			if code := p.exitCode(); code != 1 {
				t.Fatalf("終了コード = %d", code)
			}
			if got := p.stderr(); !strings.Contains(got, "中止しました") {
				t.Fatalf("標準エラー: %q", got)
			}
			if _, err := os.Stat(dir); err == nil {
				t.Fatal("中止なのにフォルダ・ファイルが作られました")
			}
		})
	}
	t.Run("既存ファイルも変わらない", func(t *testing.T) {
		tm := tmuxtest.New(t)
		home := newHome(t)
		cfg := filepath.Join(home, "config.toml")
		const src = "# 手書き\nlisten_addr = \"127.0.0.1:1\"\n"
		if err := os.WriteFile(cfg, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		p := startSetup(t, tm, home, cfg, "")
		p.listenKeep()
		p.text("sessions（カンマ区切り）")
		p.typeText("x")
		p.keys("Escape")
		if code := p.exitCode(); code != 1 {
			t.Fatalf("終了コード = %d", code)
		}
		if got := readFile(t, cfg); got != src {
			t.Fatalf("書き換えました: %q", got)
		}
	})
}

// writeFile はテストの一時フォルダにファイルを書く。
func writeFile(t *testing.T, path string, body []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, body, perm); err != nil { //nolint:gosec // テストの一時フォルダ
		t.Fatal(err)
	}
}
