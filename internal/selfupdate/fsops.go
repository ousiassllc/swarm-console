package selfupdate

import (
	"io"
	"os"

	"github.com/ousiassllc/swarm-console/internal/store"
)

// StepFile は .new への書き込み先。
type StepFile interface {
	io.Writer
	Sync() error
	Close() error
}

// FS は入れ替えの各ステップが使うファイル操作（テストが各ステップの失敗を作るために差し替える。既定は OSFS）。
type FS interface {
	Remove(path string) error
	Link(oldname, newname string) error
	Rename(oldpath, newpath string) error
	// Create は path を新規に作る（既存なら失敗）。権限は umask によらず perm にする。
	Create(path string, perm os.FileMode) (StepFile, error)
	// WriteFileAtomic は一時ファイル → fsync → rename でマーカーを書く（失敗しても書きかけを残さない）。
	WriteFileAtomic(path string, data []byte, perm os.FileMode) error
}

// OSFS は実際のファイル操作。
type OSFS struct{}

// Remove は無いファイルを成功扱いで消す。
func (OSFS) Remove(path string) error { return removeIfExists(path) }

// Link は os.Link。
func (OSFS) Link(oldname, newname string) error { return os.Link(oldname, newname) }

// Rename は os.Rename。
func (OSFS) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

// Create は path を排他的に作り、権限を perm にする。
func (OSFS) Create(path string, perm os.FileMode) (StepFile, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // Dir(home) 配下の固定名
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return f, nil
}

// WriteFileAtomic は store.WriteFileAtomic に委譲する。
func (OSFS) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return store.WriteFileAtomic(path, data, perm)
}
