package pane

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

func TestReportPathAndExists(t *testing.T) {
	if got := ReportPath("/r", "work:B2"); got != "/r/work_B2.md" {
		t.Fatalf("got %q", got)
	}
	fsys := panetest.NewMemFS()
	if ReportExists(fsys, "/r", "T1") {
		t.Fatal("無いのに存在扱い")
	}
	fsys.Put("/r/T1.md", "") // 空ファイルも存在扱い
	if !ReportExists(fsys, "/r", "T1") {
		t.Fatal("空ファイルが存在扱いにならない")
	}
}

func TestArchiveReport(t *testing.T) {
	fsys := panetest.NewMemFS()
	now := time.Unix(1700000000, 0)
	if moved, err := ArchiveReport(fsys, "/r", "work:B2", now); err != nil || moved {
		t.Fatalf("ファイルが無いのに moved=%v err=%v", moved, err)
	}
	fsys.Put("/r/work_B2.md", "報告")
	moved, err := ArchiveReport(fsys, "/r", "work:B2", now)
	if err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if mode, ok := fsys.DirMode("/r/archive"); !ok || mode != 0o700 {
		t.Fatalf("archive の権限 %v %v", mode, ok)
	}
	if !fsys.Has("/r/archive/work_B2-1700000000.md") || fsys.Has("/r/work_B2.md") {
		t.Fatalf("files = %v", fsys.Files())
	}
	// 同じ秒に同名があれば -1・-2 を足す。
	for i, want := range []string{"/r/archive/work_B2-1700000000-1.md", "/r/archive/work_B2-1700000000-2.md"} {
		fsys.Put("/r/work_B2.md", "報告"+string(rune('a'+i)))
		if _, err := ArchiveReport(fsys, "/r", "work:B2", now); err != nil {
			t.Fatal(err)
		}
		if !fsys.Has(want) {
			t.Fatalf("%s が無い: %v", want, fsys.Files())
		}
	}
}

// P-30: 最終報告のリンクは辿らない。リンクは「無い」扱いで、アーカイブ先の重複判定にも辿らない。
func TestReportExistsDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ReportPath(dir, "T1")); err != nil {
		t.Fatal(err)
	}
	if ReportExists(OSReportFS{}, dir, "T1") {
		t.Fatal("リンクを辿って存在扱いにした")
	}
	if err := os.WriteFile(ReportPath(dir, "T2"), []byte("報告"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ReportExists(OSReportFS{}, dir, "T2") {
		t.Fatal("通常ファイルが存在扱いにならない")
	}
	// アーカイブ先に壊れたリンクがあっても、辿らずに「ある」と見て（Stat なら無いと見て上書きしてしまう）別名にする。
	now := time.Unix(1700000000, 0)
	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "archive", "T2-1700000000.md")); err != nil {
		t.Fatal(err)
	}
	if moved, err := ArchiveReport(OSReportFS{}, dir, "T2", now); err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "archive", "T2-1700000000-1.md")); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("リンクを上書きせず -1 に移るはず: %v", err)
	}
}
