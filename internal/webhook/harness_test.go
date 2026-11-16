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
	// onSleep は再送の待ちに入るたびに呼ぶ（受け手の復旧・終了の再現に使う）。
	onSleep func()
}

const reportPath = reportDir + "/0_T2.md"

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, fs: &fakeFS{files: map[string]reportFile{}}, logs: &bytes.Buffer{}, status: http.StatusOK,
		state: filepath.Join(t.TempDir(), "webhook-state.json"), at: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.hits = append(h.hits, hit{r.Header.Clone(), body})
		code := h.status
		h.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(h.srv.Close)
	h.cfg = config.Webhook{
		URL: config.Secret(h.srv.URL + "/hook/" + secretPath), Secret: secretKey, Token: secretToken,
		MaxAttempts: 3, RetryInterval: 30 * time.Second, Timeout: 5 * time.Second,
	}
	return h
}

// start は台帳（h.state）から Notifier を作って Start する。ctx の終了は t.Cleanup で行う。
func (h *harness) start() context.CancelFunc {
	h.t.Helper()
	n, err := New(Options{
		Config: h.cfg, ReportDir: reportDir, StatePath: h.state, Version: "test", FS: h.fs,
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
	ctx, cancel := context.WithCancel(context.Background())
	wait := n.Start(ctx)
	h.t.Cleanup(func() { cancel(); wait() })
	return cancel
}

func (h *harness) pane(state agent.State) pane.PaneState {
	return pane.PaneState{
		ID: "%3", Session: "0", Pos: "T2", Local: "T2", GlobalPos: "0:T2", State: state,
		Note: &notes.Summary{From: "そら", Repo: "ousiassllc/swarm-console", Items: []string{"#93"}, Next: "PR を出す"},
	}
}

// observe は 5 秒進めて、state のペイン 1 つのスナップショットを渡し、始まった送信が終わるまで待つ。
func (h *harness) observe(state agent.State) {
	h.t.Helper()
	h.at = h.at.Add(5 * time.Second)
	h.n.Observe(&pane.Snapshot{At: h.at, Panes: []pane.PaneState{h.pane(state)}})
	h.n.wg.Wait()
}

// report は最終報告ファイルを（更新時刻 mtime 秒で）置く。
func (h *harness) report(mtimeSec int64) {
	h.fs.put(reportPath, time.Unix(mtimeSec, 0), 100+mtimeSec)
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
