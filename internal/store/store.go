// Package store は原子的なファイル書き込みと権限検査を提供する。
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WriteFileAtomic は同一ディレクトリの一時ファイルに書き、fsync して rename する。
// 途中で落ちても path は旧内容か新内容のどちらかで、半端な内容にならない。
// ディレクトリが無ければ 0700 で作る。ファイル権限は perm。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // 呼び出し元が決めた保存先ディレクトリ
	if err != nil {
		return fmt.Errorf("open dir: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}

// CheckMode は path の権限が want ちょうどであることを確かめる。違えばエラー。
func CheckMode(path string, want os.FileMode) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if got := fi.Mode().Perm(); got != want {
		return fmt.Errorf("%s: 権限が %04o です（%04o が必要）", path, got, want)
	}
	return nil
}

// CheckDir は path が「シンボリックリンクでないディレクトリで、持ち主が uid」であることを確かめる。
// 違えばエラー（呼び出し側は起動を拒否する）。権限が want と違うだけなら Chmod で want に直す
// （エージェントが mkdir -p しただけの 0755 などで、起動不能にしないため）。
func CheckDir(path string, want os.FileMode, uid int) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: シンボリックリンクです（実体のフォルダが必要）", path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s: フォルダではありません", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: 持ち主を調べられません", path)
	}
	if int(st.Uid) != uid {
		return fmt.Errorf("%s: 持ち主が uid %d です（uid %d が必要）", path, st.Uid, uid)
	}
	if fi.Mode().Perm() != want {
		if err := os.Chmod(path, want); err != nil {
			return fmt.Errorf("%s: 権限を %04o に直せません: %w", path, want, err)
		}
	}
	return nil
}
