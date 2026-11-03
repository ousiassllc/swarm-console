package filegettests_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

func TestFileGetTooLarge(t *testing.T) {
	e := newFileGetEnv(t)
	e.Cfg.FileGetMaxBytes = 64
	exact := writeFile(t, filepath.Join(e.work, "exact.png"), append(append([]byte{}, pngBytes...), make([]byte, 64-len(pngBytes))...))
	over := writeFile(t, filepath.Join(e.work, "over.png"), append(append([]byte{}, pngBytes...), make([]byte, 65-len(pngBytes))...))
	if got, err := fileGet(t, e.Fixture, exact); err != nil || got["size"] != float64(64) {
		t.Fatalf("ちょうど上限 = %v, %v", got, err)
	}
	d := wantCode(t, mustErr(fileGet(t, e.Fixture, over)), apperr.TooLarge)
	if d["max"] != int64(64) {
		t.Errorf("details = %v", d)
	}
	// 上限を上げれば通る（config の値が効く）。
	e.Cfg.FileGetMaxBytes = 65
	if _, err := fileGet(t, e.Fixture, over); err != nil {
		t.Errorf("上限 65 で 65 バイト = %v", err)
	}
}

func mustErr(_ map[string]any, err error) error { return err }

func TestFileGetRejectsUnsupportedType(t *testing.T) {
	e := newFileGetEnv(t)
	tests := []struct {
		name   string
		data   []byte
		reason string
	}{
		{"notes.txt", []byte("hello"), "extension"},
		{"vector.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), "extension"},
		{"clip.mov", mp4Bytes, "extension"},
		{"noext", pngBytes, "extension"},
		{"fake.png", []byte("これは画像ではありません"), "content"},
		{"empty.png", nil, "content"},
		{"short.gif", []byte("GIF"), "content"},
		{"wrong.jpg", pngBytes, "content"}, // 拡張子と中身の形式が合わない
		{"wrong.mp4", gifBytes, "content"},
		{"riff.webp", []byte("RIFF\x10\x00\x00\x00WAVEfmt "), "content"},
		{"html.png", []byte("<html><script>alert(1)</script></html>"), "content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(e.work, tt.name), tt.data)
			_, err := fileGet(t, e.Fixture, path)
			wantReason(t, wantCode(t, err, apperr.UnsupportedType), tt.reason)
		})
	}
}

func TestFileGetRejectsNonRegularFiles(t *testing.T) {
	e := newFileGetEnv(t)
	dir := filepath.Join(e.work, "dir.png")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(e.work, "pipe.png")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo できません: %v", err)
	}
	for _, p := range []string{dir, fifo} {
		_, err := fileGet(t, e.Fixture, p) // FIFO を開いて固まらない
		wantReason(t, wantCode(t, err, apperr.UnsupportedType), "not_regular")
	}
}

func TestFileGetNotFound(t *testing.T) {
	e := newFileGetEnv(t)
	for _, p := range []string{
		filepath.Join(e.work, "none.png"),
		filepath.Join(e.work, "no", "such", "dir", "none.png"),
		filepath.Join(e.Cfg.ReportDir, "none.png"),
	} {
		_, err := fileGet(t, e.Fixture, p)
		wantCode(t, err, apperr.NotFound)
	}
}

func TestFileGetInvalidArgs(t *testing.T) {
	e := newFileGetEnv(t)
	for _, args := range []string{`{}`, `{"path":""}`, `{"path":"/x\u0000.png"}`, `{"path":"a","extra":1}`, `{"path":1}`, `{"Path":"/a.png"}`, `[]`} {
		_, err := call(t, e.Fixture, "file_get", args)
		wantCode(t, err, apperr.InvalidArgs)
	}
}

// 触れないセッションのペインの cwd は許可リストに入らない。
func TestFileGetIgnoresCwdOfForbiddenSessions(t *testing.T) {
	e := newFileGetEnv(t)
	e.UseTwoSessions()
	last := len(e.Registry.Snap.Panes) - 1
	e.Registry.Snap.Panes[last].Cwd = e.outside
	path := writeFile(t, filepath.Join(e.outside, "shot.png"), pngBytes)
	tool, ok := e.Tools.Lookup(tools.EndpointOps, "file_get")
	if !ok {
		t.Fatal("file_get がありません")
	}
	args := json.RawMessage(`{"path":` + quote(path) + `}`)
	if _, err := tool.Handler(context.Background(), auth.Client{Name: "s1", Scope: auth.ScopeOps, AllowedSessions: []string{"0"}}, args); err == nil {
		t.Error("セッション 1 の cwd が許可されました")
	} else {
		wantCode(t, err, apperr.PathForbidden)
	}
	if _, err := tool.Handler(context.Background(), auth.Client{Name: "s2", Scope: auth.ScopeOps, AllowedSessions: []string{"1"}}, args); err != nil {
		t.Errorf("セッション 1 のトークンが自分のペインの cwd を読めません: %v", err)
	}
}

// 名前の無い（認証を通っていない）Client は何も許可されず、ペインの cwd の根も持たない。
func TestFileGetRequiresAuthenticatedClientForPaneRoots(t *testing.T) {
	e := newFileGetEnv(t)
	path := writeFile(t, filepath.Join(e.work, "shot.png"), pngBytes)
	tool, _ := e.Tools.Lookup(tools.EndpointOps, "file_get")
	_, err := tool.Handler(context.Background(), auth.Client{}, json.RawMessage(`{"path":`+quote(path)+`}`))
	wantCode(t, err, apperr.PathForbidden)
}
