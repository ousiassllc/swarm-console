// Package configtest はテストが config.toml の report_dir に使うフォルダを用意する。
// report_dir は /tmp/swarm-console の下だけ許される（NFR-SEC-14。config.ReportRoot）ので、t.TempDir() は使えない。
package configtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
)

// ReportDir は /tmp/swarm-console の下にテスト専用の親フォルダ（0700）を作り、その中の "reports"（まだ無い）の絶対パスを返す。
// 親フォルダはテストの終了時に消す（ほかのテスト・動いている swarm-console のフォルダには触れない）。
func ReportDir(t testing.TB) string {
	t.Helper()
	if err := os.MkdirAll(config.ReportRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(config.ReportRoot, "test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	return filepath.Join(base, "reports")
}
