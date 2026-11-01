package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// freeAddr は空いているループバックのアドレス。
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

// waitListening は addr に繋がるまで、上限つきで待つ。
func waitListening(ctx context.Context, t *testing.T, addr string, done <-chan int) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case code := <-done:
			t.Fatalf("serve が先に終了しました (exit %d)", code)
		case <-deadline:
			t.Fatal("serve が待ち受けを始めませんでした")
		case <-tick.C:
			var d net.Dialer
			if c, err := d.DialContext(ctx, "tcp", addr); err == nil {
				_ = c.Close()
				return
			}
		}
	}
}

// serve → MCP クライアントで両窓口に接続 → シグナル相当のキャンセルで終了、までを通しで確かめる。
// tmux は専用ソケット（存在しないサーバー）だけを見る。HOME は一時フォルダに差し替え、本物のデータに触れない。
func TestServeEndToEnd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	addr := freeAddr(t)
	cfg := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf("listen_addr = %q\ntmux_socket = %q\nreport_dir = %q\nallowed_roots = [%q]\nfile_roots = [%q]\n",
		addr, fmt.Sprintf("sc-serve-test-%d", os.Getpid()), filepath.Join(dir, "reports"), dir, dir)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, opsOut, _ := runCLI(t, "token", "add", "boss", "--scope", "ops", "--config", cfg)
	_, repOut, _ := runCLI(t, "token", "add", "viewer", "--scope", "report", "--config", cfg)
	opsToken, repToken := strings.TrimSpace(opsOut), strings.TrimSpace(repOut)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stderr lockedBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"serve", "--config", cfg}, &bytes.Buffer{}, &stderr) }()
	waitListening(ctx, t, addr, done)

	connect := func(path, token string) *mcp.ClientSession {
		c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil)
		cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint: "http://" + addr + path, HTTPClient: &http.Client{Transport: bearerTransport{token}},
			DisableStandaloneSSE: true, MaxRetries: -1,
		}, nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Cleanup(func() { _ = cs.Close() })
		return cs
	}
	for _, c := range []struct {
		path, token string
		want        int
	}{{"/report", repToken, 9}, {"/report", opsToken, 9}, {"/ops", opsToken, 17}} {
		res, err := connect(c.path, c.token).ListTools(ctx, nil)
		if err != nil || len(res.Tools) != c.want {
			t.Errorf("%s の tools/list: %d 個 (%v), want %d", c.path, len(res.Tools), err, c.want)
		}
	}
	// 実のスナップショット（tmux サーバーなし）でも host_status が返る。
	res, err := connect("/ops", opsToken).CallTool(ctx, &mcp.CallToolParams{Name: "host_status", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("host_status: %v %+v", err, res)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, `"tmux_session":false`) {
		t.Errorf("host_status = %s", text)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("serve の終了コード = %d\n%s", code, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serve が終了しません")
	}
	logs := stderr.String()
	for _, secret := range []string{opsToken, repToken, "Authorization"} {
		if strings.Contains(logs, secret) {
			t.Errorf("ログに秘密が出ています: %q", secret)
		}
	}
	if !strings.Contains(logs, `"msg":"audit"`) {
		t.Errorf("監査ログが標準エラーに出ていません:\n%s", logs)
	}
}

// 起動時の検証: 壊れた設定・権限が 600 でない tokens.toml は起動拒否（終了コード 1、待ち受けない）。
func TestServeRefusesBadSetup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfg := filepath.Join(dir, "config.toml")
	write := func(name, body string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		// umask で mode が削られても、検査対象の権限になるようにする。
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	serve := func() (int, string) {
		var out, errb bytes.Buffer
		code := run(context.Background(), []string{"serve", "--config", cfg}, &out, &errb)
		return code, errb.String()
	}
	write("config.toml", "listen_addr = \n", 0o600)
	if code, msg := serve(); code != 1 || !strings.Contains(msg, "エラー") {
		t.Errorf("壊れた設定: exit %d %s", code, msg)
	}
	write("config.toml", fmt.Sprintf("listen_addr = %q\n", freeAddr(t)), 0o600)
	write("tokens.toml", "", 0o644)
	if code, msg := serve(); code != 1 || !strings.Contains(msg, "tokens.toml") {
		t.Errorf("権限 644 の tokens.toml: exit %d %s", code, msg)
	}
	write("tokens.toml", "", 0o600)
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("templates/report.ja.md", "{{ .Nope", 0o600)
	if code, msg := serve(); code != 1 || !strings.Contains(msg, "テンプレート") {
		t.Errorf("構文エラーのテンプレート: exit %d %s", code, msg)
	}
	if code, _, _ := runCLI(t, "serve", "extra", "--config", cfg); code != 2 {
		t.Errorf("位置引数つきの serve = %d, want 2", code)
	}
}
