package auth

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

var baseTime = time.Date(2026, 10, 31, 22, 0, 0, 0, time.FixedZone("JST", 9*3600))

// fixture は Add で作ったダミートークン（平文はテスト内で生成したものだけ）を持つ。
type fixture struct {
	path   string
	clk    *clock.Fake
	tokens map[string]string // name -> 平文
	a      *Authenticator
}

// newFixture は report "rep" と ops "adm" を持つ tokens.toml を作って開く。
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		path:   filepath.Join(t.TempDir(), "conf", "tokens.toml"),
		clk:    clock.NewFake(baseTime),
		tokens: map[string]string{},
	}
	f.add(t, "rep", ScopeReport)
	f.add(t, "adm", ScopeOps)
	f.open(t)
	return f
}

func (f *fixture) add(t *testing.T, name string, scope Scope) {
	t.Helper()
	tok, err := Add(f.path, name, scope, f.clk.Now())
	if err != nil {
		t.Fatalf("Add(%s): %v", name, err)
	}
	f.tokens[name] = tok
}

func (f *fixture) open(t *testing.T) {
	t.Helper()
	a, err := Open(f.path, f.clk)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	f.a = a
}

func (f *fixture) bearer(name string) string { return "Bearer " + f.tokens[name] }

// bumpMtime は mtime を確実に進める（ファイルシステムの時刻粒度に依存しない）。
func bumpMtime(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mt := fi.ModTime().Add(time.Hour)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func writeRaw(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // umask の影響を避ける
		t.Fatal(err)
	}
}

// requireUnauthorized は err が details も含めて常に同一の unauthorized であることを確かめる。
func requireUnauthorized(t *testing.T, err error) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("*apperr.Error ではありません: %v", err)
	}
	if !reflect.DeepEqual(ae, apperr.New(apperr.Unauthorized)) {
		t.Fatalf("unauthorized と同一ではありません: %#v", ae)
	}
}

func requireCode(t *testing.T, err error, want apperr.Code) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != want {
		t.Fatalf("code = %v, want %s", err, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // テストが t.TempDir() 内に作ったパス
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
