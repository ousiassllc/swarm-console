package safety

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// 開いたファイルの実パスが検証した実パスと違えば断る（検証後に途中のディレクトリがリンクへ差し替えられた場合の備え）。
func TestCheckOpenedRejectsSwappedDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "dir")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{inside, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(inside, "a.png"), filepath.Join(outside, "a.png")} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	resolved := filepath.Join(inside, "a.png")

	open := func() *os.File {
		f, err := os.Open(resolved) //nolint:gosec // テストの一時ファイル
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}

	// 差し替えがなければ通る。
	if err := CheckOpened(open(), resolved); err != nil {
		t.Fatalf("差し替えなし: %v", err)
	}

	// 検証のあとに dir が外へのリンクに差し替えられると、同じパスで外のファイルが開く。
	if err := os.Rename(inside, filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Fatal(err)
	}
	if err := CheckOpened(open(), resolved); err == nil || apperr.As(err).Code != apperr.PathForbidden {
		t.Fatalf("差し替え後 = %v, want path_forbidden", err)
	}
}

// /proc/self/fd を読めないとき（閉じた fd など）は確かめられないので、素通しにせず internal で断る
// （仕様書の endpoints-pane.md §file_get・NFR-SEC-17。Linux の /proc が必須）。
func TestCheckOpenedFailsWhenProcUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close() // 閉じた File の Fd() は無効な値になり、/proc/self/fd/<fd> を読めない
	if err := CheckOpened(f, path); err == nil || apperr.As(err).Code != apperr.Internal {
		t.Fatalf("読めないとき = %v, want internal", err)
	}
}
