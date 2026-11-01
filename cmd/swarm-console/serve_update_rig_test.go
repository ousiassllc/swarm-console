package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/updatecheck/updatechecktest"
)

const (
	oldBin = "OLD-BINARY"
	newBin = "NEW-BINARY-v0.1.5"
)

var clkStart = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// serveRig は一時 HOME の置き場（~/bin/swarm-console）・偽の GitHub・偽の時計で動かす serve。
type serveRig struct {
	t      *testing.T
	home   string
	binDir string
	exe    string
	cfg    string
	addr   string
	token  string
	origin *updatechecktest.Origin
	clk    *clock.Fake
	stderr *lockedBuffer
	cancel context.CancelFunc
	done   chan int
	ctx    context.Context
	values *valuesRig
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// newServeRig は HOME・置き場・設定・トークンを用意する（serve はまだ起動しない）。
func newServeRig(t *testing.T, current, extraConfig string) *serveRig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	g := &serveRig{t: t, home: home, binDir: filepath.Join(home, "bin", "swarm-console"), addr: freeAddr(t),
		cfg: filepath.Join(home, "config.toml"), origin: updatechecktest.New(t), clk: clock.NewFake(clkStart), stderr: &lockedBuffer{}}
	if err := os.MkdirAll(g.binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	g.exe = filepath.Join(g.binDir, "swarm-console")
	g.writeFile(g.exe, oldBin, 0o755)
	body := fmt.Sprintf("listen_addr = %q\ntmux_socket = %q\nreport_dir = %q\nallowed_roots = [%q]\nfile_roots = [%q]\n%s",
		g.addr, fmt.Sprintf("sc-upd-test-%d", os.Getpid()), filepath.Join(home, "reports"), home, home, extraConfig)
	g.writeFile(g.cfg, body, 0o600)
	_, out, _ := runCLI(t, "token", "add", "boss", "--scope", "ops", "--config", g.cfg)
	g.token = strings.TrimSpace(out)
	// 収集器（omp の実行）は、実際の omp を呼ばない偽の Runner と、確認とは別の時計で動かす。
	g.values = newValuesRig()
	useVersion(t, current, app.UpdateEnv{
		Home: home, Transport: g.origin.Transport(), Clock: g.clk,
		ValuesRunner: g.values, ValuesClock: g.values.clk,
		Executable: func() (string, error) { return g.exe, nil },
		FirstDelay: 0,
	})
	return g
}

func (g *serveRig) writeFile(path, content string, mode os.FileMode) {
	g.t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		g.t.Fatal(err)
	}
}

func (g *serveRig) read(name string) string {
	b, err := os.ReadFile(filepath.Join(g.binDir, name)) //nolint:gosec // テストが作った一時ディレクトリ
	if err != nil {
		return ""
	}
	return string(b)
}

func (g *serveRig) exists(name string) bool {
	_, err := os.Lstat(filepath.Join(g.binDir, name))
	return err == nil
}

// files は置き場のファイル名（一時ファイルが残っていないことの確認用）。
func (g *serveRig) files() []string {
	ents, _ := os.ReadDir(g.binDir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// release は最新版 tag の偽の GitHub（API・SHA256SUMS・バイナリ）を用意する。
func (g *serveRig) release(tag, content string) {
	g.origin.HandleFunc("api.github.com", releaseJSON(tag))
	asset := "swarm-console_" + tag + "_linux_amd64"
	prefix := "/ousiassllc/swarm-console/releases/download/" + tag + "/"
	g.origin.HandleFunc("github.com", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prefix + "SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  %s\n", sha(content), asset)
		case prefix + asset:
			_, _ = w.Write([]byte(content))
		default:
			http.NotFound(w, r)
		}
	})
}

// start は serve を起動し、待ち受けるまで待つ。
func (g *serveRig) start() {
	g.t.Helper()
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.t.Cleanup(g.cancel)
	g.done = make(chan int, 1)
	go func() { g.done <- run(g.ctx, []string{"serve", "--config", g.cfg}, &bytes.Buffer{}, g.stderr) }()
	waitListening(g.ctx, g.t, g.addr, g.done)
}

// wait は serve が終わるまで待ち、終了コードを返す。
func (g *serveRig) wait() int {
	g.t.Helper()
	select {
	case code := <-g.done:
		return code
	case <-time.After(20 * time.Second):
		g.t.Fatalf("serve が終了しません\n%s", g.stderr.String())
		return -1
	}
}

// hostUpdate は host_status の update を返す（MCP 経由。呼び出しで GitHub へは通信しない）。
func (g *serveRig) hostUpdate() map[string]any {
	g.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "upd", Version: "1"}, nil)
	cs, err := c.Connect(g.ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://" + g.addr + "/report", HTTPClient: &http.Client{Transport: bearerTransport{g.token}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		g.t.Fatalf("接続: %v", err)
	}
	defer func() { _ = cs.Close() }()
	res, err := cs.CallTool(g.ctx, &mcp.CallToolParams{Name: "host_status", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		g.t.Fatalf("host_status: %v %+v", err, res)
	}
	var out struct {
		Update map[string]any `json:"update"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil || out.Update == nil {
		g.t.Fatalf("update がありません: %v", err)
	}
	return out.Update
}

func (g *serveRig) apply(u map[string]any) map[string]any {
	a, _ := u["apply"].(map[string]any)
	return a
}

// untilUpdate は host_status.update の status が want になるまで、上限つきで繰り返す。
func (g *serveRig) untilUpdate(want string) map[string]any {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		u := g.hostUpdate()
		if u["status"] == want {
			return u
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("update.status が %s になりません: %v", want, u)
		}
		time.Sleep(10 * time.Millisecond) // 上限つきのポーリング間隔
	}
}

// untilWaiting は確認の goroutine が clock の待ちに入るまで待つ（Advance の取りこぼしを防ぐ）。
func (g *serveRig) untilWaiting(n int) {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for g.clk.Waiters() < n {
		if time.Now().After(deadline) {
			g.t.Fatalf("clock の待ちが %d になりません（%d）", n, g.clk.Waiters())
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
}

// ctxCancel はテスト終了で止まる ctx。
func ctxCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx, cancel
}

// listenOn は addr を先に塞ぐ（serve の bind 失敗を作る）。
func listenOn(t *testing.T, addr string) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return ln
}
