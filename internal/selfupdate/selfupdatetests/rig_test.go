package selfupdatetests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/selfupdate"
	"github.com/ousiassllc/swarm-console/internal/updatecheck/updatechecktest"
)

const (
	oldContent = "OLD-BINARY\n"
	newContent = "NEW-BINARY-v0.1.5\n"
	curVersion = "v0.1.4"
	newVersion = "v0.1.5"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sumsLine(tag, content string) string { return sha(content) + "  " + assetName(tag) + "\n" }

// release は 1 つのリリース（SHA256SUMS とバイナリ）を偽の配信元で返す設定。
type release struct {
	tag    string
	sums   func(w http.ResponseWriter, r *http.Request)
	binary func(w http.ResponseWriter, r *http.Request)
}

func okRelease(tag, content string) release {
	return release{
		tag:    tag,
		sums:   body(sumsLine(tag, content)),
		binary: body(content),
	}
}

func body(s string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(s)) }
}

func code(c int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "SECRET-BODY", c) }
}

// rig は一時 HOME・偽の配信元・selfupdate.Updater の一式。
type rig struct {
	t      *testing.T
	home   string
	dir    string
	exe    string
	origin *updatechecktest.Origin
	logs   *lockedBuf
	clk    *clock.Fake
	exits  atomic.Int32
	u      *selfupdate.Updater
	opts   Options
}

func newRig(t *testing.T, mutate ...func(*Options)) *rig {
	t.Helper()
	home := t.TempDir()
	dir := selfupdate.Dir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, binName)
	if err := os.WriteFile(exe, []byte(oldContent), 0o755); err != nil { //nolint:gosec // 実行ファイルを模した一時ファイル
		t.Fatal(err)
	}
	g := &rig{t: t, home: home, dir: dir, exe: exe, origin: updatechecktest.New(t), logs: &lockedBuf{}, clk: clock.NewFake(t0)}
	g.opts = Options{
		Home: home, Current: curVersion, GOOS: "linux", GOARCH: "amd64",
		Executable: func() (string, error) { return exe, nil },
		Transport:  g.origin.Transport(), Timeout: 20 * time.Second, Clock: g.clk,
		Logger:      slog.New(slog.NewJSONHandler(g.logs, nil)),
		RequestExit: func() { g.exits.Add(1) },
	}
	for _, m := range mutate {
		m(&g.opts)
	}
	u, err := selfupdate.New(g.opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	g.u = u
	return g
}

// serve は github.com の releases/download/<tag>/ を r で返す。
func (g *rig) serve(r release) {
	prefix := "/ousiassllc/swarm-console/releases/download/" + r.tag + "/"
	g.origin.HandleFunc("github.com", func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case prefix + "SHA256SUMS":
			r.sums(w, req)
		case prefix + assetName(r.tag):
			r.binary(w, req)
		default:
			http.NotFound(w, req)
		}
	})
}

func (g *rig) read(name string) string {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(g.dir, name)) //nolint:gosec // テストが作った一時ディレクトリ
	if err != nil {
		g.t.Fatal(err)
	}
	return string(b)
}

func (g *rig) exists(name string) bool {
	_, err := os.Lstat(filepath.Join(g.dir, name))
	return err == nil
}

// files は置き場にあるファイル名（並べ替え済み）。5 ファイル以外・一時ファイルが残っていないことの確認に使う。
func (g *rig) files() []string {
	g.t.Helper()
	ents, err := os.ReadDir(g.dir)
	if err != nil {
		g.t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// assertUntouched は本体が旧バイナリのままで、置き場に本体以外が残っていない（掃除済み）ことを確かめる。
func (g *rig) assertUntouched() {
	g.t.Helper()
	if got := g.read(binName); got != oldContent {
		g.t.Errorf("本体が変わっています: %q", got)
	}
	if got := g.files(); !slices.Equal(got, []string{binName}) {
		g.t.Errorf("置き場のファイル = %v, want [%s]（一時ファイル・マーカーが残っています）", got, binName)
	}
	if n := g.exits.Load(); n != 0 {
		g.t.Errorf("入れ替えていないのに終了を要求しました（%d 回）", n)
	}
}

func (g *rig) apply(tag string) error { return g.u.Apply(context.Background(), tag) }

func wantFailure(t *testing.T, err error, reason Reason) {
	t.Helper()
	f, ok := err.(*selfupdate.Failure) //nolint:errorlint // 型そのものを確かめる
	if !ok || f.Reason != reason {
		t.Fatalf("err = %v, want failure %s", err, reason)
	}
}

func (g *rig) wantApply(state State, reason Reason) {
	g.t.Helper()
	if a := g.u.Status(); a.State != state || a.Reason != reason {
		g.t.Fatalf("apply = %+v, want %s/%s", a, state, reason)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("条件が成り立ちません: %s", what)
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
}
