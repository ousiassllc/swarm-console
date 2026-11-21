package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckDir(t *testing.T) {
	uid := os.Getuid()
	t.Run("自分の 0700 のフォルダは通る", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "d")
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := CheckDir(d, 0o700, uid); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("権限が緩いだけなら 0700 に直す", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "d")
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(d, 0o750); err != nil { //nolint:gosec // umask を避け、緩い権限を作る
			t.Fatal(err)
		}
		if err := CheckDir(d, 0o700, uid); err != nil {
			t.Fatal(err)
		}
		if err := CheckMode(d, 0o700); err != nil {
			t.Fatalf("0700 に直っていません: %v", err)
		}
	})
	t.Run("持ち主が違えば拒否し、権限も触らない", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "d")
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(d, 0o750); err != nil { //nolint:gosec // 緩い権限を作るテスト
			t.Fatal(err)
		}
		err := CheckDir(d, 0o700, uid+1)
		if err == nil || !strings.Contains(err.Error(), "持ち主") {
			t.Fatalf("err=%v", err)
		}
		if fi, _ := os.Stat(d); fi.Mode().Perm() != 0o750 {
			t.Fatalf("拒否したのに権限を変えました: %v", fi.Mode().Perm())
		}
	})
	t.Run("シンボリックリンクは拒否", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		err := CheckDir(link, 0o700, uid)
		if err == nil || !strings.Contains(err.Error(), "シンボリックリンク") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("ファイルは拒否", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		err := CheckDir(f, 0o700, uid)
		if err == nil || !strings.Contains(err.Error(), "フォルダではありません") {
			t.Fatalf("err=%v", err)
		}
	})
}
