package safety

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// CheckFile のあと open の前に途中のディレクトリが file_roots の外へのリンクに差し替えられても、外の中身を返さず forbidden_path にする
// （開いた fd の実パスの確認 = CheckOpened が効く。send_text の語彙に直す）。
func TestReadFileRejectsDirectorySwappedBeforeOpen(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.files, "dir")
	path := write(t, filepath.Join(dir, "a.txt"), "inside")
	write(t, filepath.Join(e.outside, "a.txt"), "OUTSIDE")
	e.p.beforeOpen = func(string) {
		if err := os.Rename(dir, filepath.Join(e.files, "moved")); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(e.outside, dir); err != nil {
			t.Error(err)
		}
	}
	got, err := e.p.ReadFile(path)
	if err == nil {
		t.Fatalf("差し替えられたのに外の中身が返りました: %q", got)
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.ForbiddenPath || ae.Details["field"] != "file" || ae.Details["reason"] != "outside_roots" {
		t.Fatalf("forbidden_path(field=file, reason=outside_roots) のはずが %v", err)
	}
}
