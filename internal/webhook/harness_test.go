package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// テストで使う秘密の値。ログ・台帳・エラー文のどこにも出てはいけない。
const (
	secretToken = "TOKSECRET-5f1c" //nolint:gosec // テスト用のダミー値
	secretKey   = "SIGSECRET-9a7d" //nolint:gosec // テスト用のダミー値
	secretPath  = "URLSECRET-3b2e" //nolint:gosec // テスト用のダミー値
)

const reportDir = "/reports"

// reportFile は fakeFS 上の最終報告ファイル。
type reportFile struct {
	mtime time.Time
	size  int64
}

// fakeFS は pane.ReportFS（Lstat だけ使う）。
type fakeFS struct {
	mu    sync.Mutex
	files map[string]reportFile
}

func (f *fakeFS) put(path string, mtime time.Time, size int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = reportFile{mtime, size}
}

type fakeInfo struct {
	name string
	rf   reportFile
}

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return i.rf.size }
func (i fakeInfo) Mode() fs.FileMode  { return 0o600 }
func (i fakeInfo) ModTime() time.Time { return i.rf.mtime }
func (i fakeInfo) IsDir() bool        { return false }
func (i fakeInfo) Sys() any           { return nil }

func (f *fakeFS) Lstat(p string) (fs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rf, ok := f.files[p]
	if !ok {
		return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
	}
	return fakeInfo{filepath.Base(p), rf}, nil
}
func (*fakeFS) ReadFile(string) ([]byte, error)    { return nil, fs.ErrNotExist }
func (*fakeFS) MkdirAll(string, os.FileMode) error { return nil }
func (*fakeFS) Rename(string, string) error        { return nil }

// hit は受け手が受けた POST 1 回分。
type hit struct {
	header http.Header
	body   []byte
}

// harness は Notifier と、POST を受けて数える受け手のサーバー。
type harness struct {
	t     *testing.T
	srv   *httptest.Server
	fs    *fakeFS
	logs  *bytes.Buffer
	state string
	cfg   config.Webhook

	mu     sync.Mutex
	hits   []hit
	status int // 受け手が返す応答
	slept  []time.Duration
	n      *Notifier
	at     time.Time
	note   *notes.Summary // ペインに付くメモ（テストが途中で替えられる）
	// srvStart はスナップショットの ServerStart（tmux の起動時刻）。
	srvStart string
	un       *pane.Unaccepted   // Notifier に渡す受理の記録（nil なら渡さない）
	cancel   context.CancelFunc // 今の Notifier の ctx を止める
	wait     func()             // 今の Notifier の送信の終了を待つ
	// intercept があれば、POST を記録したあとに呼ぶ。true を返したら、受け手はそれ以上応答しない
	// （応答なしで接続を切る・通信の途中で止める再現に使う）。
	intercept func(w http.ResponseWriter, r *http.Request) bool
	// onSleep は再送の待ちに入るたびに呼ぶ（受け手の復旧・終了の再現に使う）。
	onSleep func()
}

const reportPath = reportDir + "/0_T2.md"

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, fs: &fakeFS{files: map[string]reportFile{}}, logs: &bytes.Buffer{}, status: http.StatusOK,
		state: filepath.Join(t.TempDir(), "webhook-state.json"), at: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
		note: &notes.Summary{From: "そら", Repo: "ousiassllc/swarm-console", Items: []string{"#93"}, Next: "PR を出す"}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.hits = append(h.hits, hit{r.Header.Clone(), body})
		code, intercept := h.status, h.intercept
		h.mu.Unlock()
		if intercept != nil && intercept(w, r) {
			return
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(h.srv.Close)
	h.cfg = config.Webhook{
		URL: config.Secret(h.srv.URL + "/hook/" + secretPath), Secret: secretKey, Token: secretToken,
		MaxAttempts: 3, RetryInterval: 30 * time.Second, Timeout: 5 * time.Second,
	}
	return h
}

// build は台帳（h.state）から Notifier を作る（Start はしない）。
func (h *harness) build() {
	h.t.Helper()
	n, err := New(Options{
		Config: h.cfg, ReportDir: reportDir, StatePath: h.state, Version: "test", FS: h.fs, Unaccepted: h.un,
		Logger: slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Sleep: func(ctx context.Context, d time.Duration) error {
			h.mu.Lock()
			h.slept = append(h.slept, d)
			hook := h.onSleep
			h.mu.Unlock()
			if hook != nil {
				hook()
			}
			return ctx.Err()
		},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.n = n
}

// start は台帳から Notifier を作って Start する。ctx の終了は t.Cleanup でも行う。
func (h *harness) start() context.CancelFunc {
	h.t.Helper()
	h.build()
	h.begin()
	return h.cancel
}

// begin は build した Notifier を Start する。
func (h *harness) begin() {
	ctx, cancel := context.WithCancel(context.Background())
	wait := h.n.Start(ctx)
	h.cancel, h.wait = cancel, wait
	h.t.Cleanup(func() { cancel(); wait() })
}

// restart はプロセスの再起動の再現。今の Notifier を止め、送信の終了を待ってから、同じ台帳で新しく作って Start し、
// 台帳から引き継いだ項目の送信が終わるまで待つ。
func (h *harness) restart() {
	h.t.Helper()
	h.cancel()
	h.wait()
	h.start()
	h.n.wg.Wait()
}

func (h *harness) pane(state agent.State) pane.PaneState {
	return h.paneAt("%3", "0:T2", "T2", state)
}

func (h *harness) paneAt(id, global, local string, state agent.State) pane.PaneState {
	return pane.PaneState{ID: id, Session: "0", Pos: local, Local: local, GlobalPos: global, State: state, Note: h.note}
}

// observe は 5 秒進めて、state のペイン 1 つのスナップショットを渡し、始まった送信が終わるまで待つ。
func (h *harness) observe(state agent.State) {
	h.t.Helper()
	h.snap(h.pane(state))
}

// snap は 5 秒進めて、panes のスナップショットを渡し、始まった送信が終わるまで待つ。
func (h *harness) snap(panes ...pane.PaneState) {
	h.t.Helper()
	h.at = h.at.Add(5 * time.Second)
	h.n.Observe(&pane.Snapshot{At: h.at, Panes: panes, ServerStart: h.srvStart})
	h.n.wg.Wait()
}

// report は最終報告ファイルを（更新時刻 mtime 秒で）置く。
func (h *harness) report(mtimeSec int64) { h.reportAt(reportPath, mtimeSec) }

// reportAt は path に最終報告ファイルを置く。
func (h *harness) reportAt(path string, mtimeSec int64) {
	h.fs.put(path, time.Unix(mtimeSec, 0), 100+mtimeSec)
}

func (h *harness) received() []hit {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hit(nil), h.hits...)
}

func (h *harness) setStatus(code int) {
	h.mu.Lock()
	h.status = code
	h.mu.Unlock()
}

func (h *harness) sleeps() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]time.Duration(nil), h.slept...)
}

func (h *harness) payload(i int) Payload {
	h.t.Helper()
	hits := h.received()
	if i >= len(hits) {
		h.t.Fatalf("POST が %d 回しか届いていません（%d 番目を読もうとした）", len(hits), i)
	}
	var pl Payload
	if err := json.Unmarshal(hits[i].body, &pl); err != nil {
		h.t.Fatal(err)
	}
	return pl
}
