package harness

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

// Session は harness が作る tmux セッション名（config.toml の sessions の先頭）。
const Session = "main"

// Options は Start の設定。ゼロ値で「120x40 の 1 セッション・サンプラーあり」。
type Options struct {
	NoSampler     bool                         // true なら app.Run ではなく HTTP サーバーだけを動かす（スナップショットは読み取りのときだけ作る）
	NoTmuxSession bool                         // true なら tmux のセッションを作らない
	Width, Height int                          // 最初のセッションの大きさ（既定 120x40）
	Config        map[string]string            // config.toml の最上位キーの追加・上書き（値は TOML のリテラル。"" ならそのキーを書かない）
	Agents        map[string]map[string]string // [agents.<名前>] のキーの追加・上書き（値は TOML のリテラル）
	Profiles      string                       // あれば profiles.toml の中身
	// ExtraSessions は main のほかに作る tmux セッション（config.toml の sessions に main のあとへ並べる。default_session は main）。
	ExtraSessions []string
	// TokenSessions は、トークン名（OpsA など）ごとの allowed_sessions。無いトークンは書かない（config の sessions すべて）。
	TokenSessions map[string][]string
}

// H は起動済みの swarm-console と、その専用 tmux・設定・トークン。
type H struct {
	T       *testing.T
	Dir     string // 一時フォルダ（HOME でもある）
	Work    string // allowed_roots の下の作業フォルダ
	Files   string // file_roots の下
	Outside string // どの許可の根にも入らないフォルダ
	Reports string // report_dir
	Logs    string // 擬似エージェントの --log の置き場
	Addr    string
	Config  string // config.toml のパス
	TM      *tmuxtest.Server
	P0      string            // 最初のセッションの最初のペイン（シェル）
	PExtra  map[string]string // ExtraSessions の各セッションの最初のペイン（シェル）
	App     *app.App

	tokens map[string]string
	buf    *lockedBuffer
	cancel context.CancelFunc
	done   chan struct{} // Run（Serve）が戻ったら閉じる

	mu      sync.Mutex
	clients map[string]*Client // Ops など使い回す接続
	all     []*Client          // 張った接続すべて（停止時に閉じる）
	runErr  error              // done が閉じたあとに読む
}

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

// Token 名。ops の 2 つと report の 2 つは、別々のクライアント接続（別トークン）として使う。
const (
	OpsA    = "ops-a"
	OpsB    = "ops-b"
	ReportA = "report-a"
	ReportB = "report-b"
)

// FreeAddr は空いているループバックのアドレス。
func FreeAddr(t testing.TB) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

// Prepare は一時フォルダ・HOME・PATH・専用 tmux・config.toml・トークンまでを用意する（app は起動しない）。
func Prepare(t *testing.T, o Options) *H {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux が見つかりません。統合テストには tmux が必要です（NFR-TEST-8）: %v", err)
	}
	bin := Bin(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := &H{
		T: t, Dir: dir, Work: filepath.Join(dir, "work"), Files: filepath.Join(dir, "files"),
		Outside: filepath.Join(dir, "outside"), Reports: filepath.Join(dir, "reports"), Logs: filepath.Join(dir, "logs"),
		Addr: FreeAddr(t), Config: filepath.Join(dir, "config.toml"),
		tokens: map[string]string{}, buf: &lockedBuffer{}, clients: map[string]*Client{},
	}
	for _, d := range []string{h.Work, h.Files, h.Outside, h.Logs} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	h.TM = tmuxtest.New(t)
	h.writeConfig(o)
	if o.Profiles != "" {
		h.WriteFile(filepath.Join(dir, "profiles.toml"), o.Profiles)
	}
	tokensPath := filepath.Join(dir, "tokens.toml")
	for name, scope := range map[string]auth.Scope{OpsA: auth.ScopeOps, OpsB: auth.ScopeOps, ReportA: auth.ScopeReport, ReportB: auth.ScopeReport} {
		tok, err := auth.Add(tokensPath, name, scope, time.Now(), o.TokenSessions[name]...)
		if err != nil {
			t.Fatalf("トークンを発行できません: %v", err)
		}
		h.tokens[name] = tok
	}
	if !o.NoTmuxSession {
		w, hh := o.Width, o.Height
		if w == 0 {
			w, hh = 120, 40
		}
		// tmux-256color は端末の clear が履歴も消す（screen 系は消さず、pane_close の画面確認が通らない）。実機の既定に合わせる。
		out := h.TM.Run("start-server", ";", "set", "-g", "default-terminal", "tmux-256color", ";",
			"new-session", "-d", "-P", "-F", "#{pane_id}", "-s", Session, "-x", strconv.Itoa(w), "-y", strconv.Itoa(hh))
		h.P0 = strings.TrimSpace(out)
		h.PExtra = map[string]string{}
		for _, s := range o.ExtraSessions {
			h.PExtra[s] = strings.TrimSpace(h.TM.Run("new-session", "-d", "-P", "-F", "#{pane_id}", "-s", s, "-x", strconv.Itoa(w), "-y", strconv.Itoa(hh)))
		}
	}
	return h
}

// Start は Prepare のあと app を作って起動し、HTTP が応答するまで待つ。終了時に止める。
func Start(t *testing.T, o Options) *H {
	t.Helper()
	h := Prepare(t, o)
	logger := slog.New(slog.NewJSONHandler(h.buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a, err := app.New(h.Config, "integration", logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	h.App = a
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.done = make(chan struct{})
	go func() {
		defer close(h.done)
		if o.NoSampler {
			h.runErr = a.Server.Serve(ctx)
			return
		}
		h.runErr = a.Run(ctx)
	}()
	t.Cleanup(func() {
		_ = h.Stop() // 戻り値の確認は各テスト（停止のテスト）が行う
		h.checkLogsHaveNoSecrets()
	})
	h.waitReady()
	return h
}

// waitReady は POST /ops（認証なし）に応答が返るまで、上限つきで繰り返す。
func (h *H) waitReady() {
	h.T.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-h.done:
			h.T.Fatalf("サーバーが先に終了しました: %v", h.runErr)
		default:
		}
		if _, _, err := h.post("/ops", "", "{}"); err == nil {
			return
		}
		if time.Now().After(deadline) {
			h.T.Fatal("サーバーが待ち受けを始めません")
		}
		time.Sleep(10 * time.Millisecond) // 上限つきのポーリング間隔
	}
}

// Stop は ctx をキャンセルして Run（Serve）が戻るのを待ち、その戻り値を返す。2 回目以降も同じ値を返す。
func (h *H) Stop() error {
	h.T.Helper()
	h.mu.Lock()
	for _, c := range h.all {
		_ = c.CS.Close()
	}
	h.all = nil
	h.mu.Unlock()
	h.cancel()
	return h.Wait(60 * time.Second)
}

// Cancel は停止を始めるだけで待たない（進行中の操作がある状態の停止を確かめるため）。
func (h *H) Cancel() { h.cancel() }

// Wait は Run（Serve）が戻るまで最大 limit 待ち、その戻り値を返す。戻らなければテストを止める。
func (h *H) Wait(limit time.Duration) error {
	h.T.Helper()
	select {
	case <-h.done:
		return h.runErr
	case <-time.After(limit):
		h.T.Fatalf("サーバーが %s 以内に停止しません", limit)
		return nil
	}
}

func (h *H) checkLogsHaveNoSecrets() {
	logs := h.buf.String()
	for name, tok := range h.tokens {
		if strings.Contains(logs, tok) {
			h.T.Errorf("ログにトークン %s の平文が出ています", name)
		}
	}
	if strings.Contains(logs, "Authorization") {
		h.T.Error("ログに Authorization ヘッダーが出ています")
	}
}

// AuditLog はこれまでのログ（JSON Lines）。
func (h *H) AuditLog() string { return h.buf.String() }

// Token は名前のトークンの平文（テストが発行した使い捨て）。
func (h *H) Token(name string) string { return h.tokens[name] }

// TokensPath は tokens.toml のパス。
func (h *H) TokensPath() string { return filepath.Join(h.Dir, "tokens.toml") }

// WriteFile はテスト用のファイルを書く（親フォルダも作る）。
func (h *H) WriteFile(path, body string) {
	h.T.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.T.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		h.T.Fatal(err)
	}
}

// Eventually は cond が true になるまで、上限（30 秒）つきで繰り返す。固定時間の待ちには使わない。
func (h *H) Eventually(what string, cond func() bool) {
	h.T.Helper()
	Eventually(h.T, what, cond)
}

// Eventually は cond が true になるまで上限つきで繰り返す。
func Eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !cond() {
		select {
		case <-deadline.C:
			t.Fatalf("条件待ちがタイムアウト: %s", what)
			return
		case <-tick.C:
		}
	}
}
