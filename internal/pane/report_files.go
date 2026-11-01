package pane

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReportFS は最終報告ファイルの出入り（ER §7）。テストではメモリ上の実装に差し替える。
type ReportFS interface {
	Stat(path string) (fs.FileInfo, error)
	ReadFile(path string) ([]byte, error)
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldPath, newPath string) error
}

// OSReportFS は os パッケージによる ReportFS。
type OSReportFS struct{}

// Stat は os.Stat。
func (OSReportFS) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }

// ReadFile は os.ReadFile。
func (OSReportFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) } //nolint:gosec // 呼び出し側が report_dir 配下の固定形式のパスだけを渡す

// MkdirAll は os.MkdirAll。
func (OSReportFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }

// Rename は os.Rename。
func (OSReportFS) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

// reportBase は位置をファイル名にしたもの。位置の `:` は `_`（P-11）。パスを抜け出せる `/` も `_` にする。
func reportBase(pos string) string {
	return strings.NewReplacer(":", "_", "/", "_", `\`, "_").Replace(pos)
}

// ReportPath は最終報告ファイルのパス（<dir>/<位置>.md）。
func ReportPath(dir, pos string) string {
	return filepath.Join(dir, reportBase(pos)+".md")
}

// ReportExists は最終報告ファイルが（空でも）通常ファイルとしてあるか。
func ReportExists(f ReportFS, dir, pos string) bool {
	fi, err := f.Stat(ReportPath(dir, pos))
	return err == nil && fi.Mode().IsRegular()
}

// ArchiveReport は古い最終報告ファイルを <dir>/archive/<位置>-<unix 秒>.md へ移す（P-11）。
// ディレクトリは 0700 で作り、同名があれば拡張子の前に -1・-2… を足す。ファイルが無ければ moved=false。
func ArchiveReport(f ReportFS, dir, pos string, now time.Time) (moved bool, err error) {
	if !ReportExists(f, dir, pos) {
		return false, nil
	}
	archive := filepath.Join(dir, "archive")
	if err := f.MkdirAll(archive, 0o700); err != nil {
		return false, fmt.Errorf("pane: アーカイブ先を作れません: %w", err)
	}
	stem := reportBase(pos) + "-" + strconv.FormatInt(now.Unix(), 10)
	dst := filepath.Join(archive, stem+".md")
	for i := 1; ; i++ {
		if _, err := f.Stat(dst); err != nil {
			break
		}
		dst = filepath.Join(archive, stem+"-"+strconv.Itoa(i)+".md")
	}
	if err := f.Rename(ReportPath(dir, pos), dst); err != nil {
		return false, fmt.Errorf("pane: 最終報告を移せません: %w", err)
	}
	return true, nil
}
