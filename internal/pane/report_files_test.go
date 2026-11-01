package pane

import (
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
