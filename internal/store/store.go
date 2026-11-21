// Package store は原子的なファイル書き込みと、フォルダの持ち主・権限・親フォルダの検査を提供する。
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

// CheckDir は path が「シンボリックリンクでないディレクトリで、持ち主が uid」であり、
// かつ親フォルダも安全であることを確かめる。違えばエラー（呼び出し側は起動を拒否する）。
// 権限が want と違うだけなら Chmod で want に直す（エージェントが mkdir -p しただけの 0755 などで、
// 起動不能にしないため）。権限を直すのは、持ち主と親フォルダの検査に通ったあと。
//
// 親フォルダの検査は checkAncestors を参照。root は uid 0 として扱う。
func CheckDir(path string, want os.FileMode, uid int) error {
	return checkDir(path, want, uid, 0)
}

// checkDir は CheckDir の本体。rootUID は「システムの持ち主」と見なす uid（本番は 0。テストが差し替える）。
func checkDir(path string, want os.FileMode, uid, rootUID int) error {
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
	if err := checkAncestors(path, uid, rootUID); err != nil {
		return err
	}
	if fi.Mode().Perm() != want {
		if err := os.Chmod(path, want); err != nil {
			return fmt.Errorf("%s: 権限を %04o に直せません: %w", path, want, err)
		}
	}
	return nil
}

// checkAncestors は path の親フォルダを、根へ向かって順に検査する。
// /tmp → /private/tmp のような祖先のシンボリックリンクは、先に解決した実パスの祖先を見る
// （リンク自体は拒否しない。攻撃者がリンクを張っても、解決先の持ち主・権限で拒否される）。
// 各親は「持ち主が uid か rootUID」で、グループ／その他が書ける場合は sticky ビット付きのものだけ許す
// （/tmp 相当。sticky なら、他人は中の他人のエントリを rename／unlink できない）。
// rootUID 所有の親（/・/home・/tmp など）に届いたら、そこで止める。
func checkAncestors(path string, uid, rootUID int) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("%s: 実パスを解決できません: %w", path, err)
	}
	for dir := filepath.Dir(resolved); ; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("親フォルダ %s を調べられません: %w", dir, err)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("親フォルダ %s: 持ち主を調べられません", dir)
		}
		owner := int(st.Uid)
		if owner != uid && owner != rootUID {
			return fmt.Errorf("親フォルダ %s: 持ち主が uid %d です（uid %d か root が必要）", dir, owner, uid)
		}
		if fi.Mode().Perm()&0o022 != 0 && fi.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("親フォルダ %s: 他のユーザーが書ける権限（%04o）です", dir, fi.Mode().Perm())
		}
		if owner == rootUID || dir == filepath.Dir(dir) {
			return nil
		}
	}
}
