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

// mkdirMode は umask に左右されず mode のフォルダを作る。
func mkdirMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// 他ユーザー所有のフォルダは作れないので、uid と「root と見なす uid」を引数で差し替えて確かめる。
// 作ったフォルダの持ち主は常にテストを動かす uid（me）。
func TestCheckDirAncestors(t *testing.T) {
	me := os.Getuid()
	other := me + 1
	// 親 parent の下に leaf（自分の 0700）を作る。
	setup := func(t *testing.T, parentMode os.FileMode) (parent, leaf string) {
		t.Helper()
		parent = filepath.Join(t.TempDir(), "parent")
		mkdirMode(t, parent, parentMode)
		leaf = filepath.Join(parent, "leaf")
		mkdirMode(t, leaf, 0o700)
		return parent, leaf
	}
	reject := func(t *testing.T, leaf string, uid, rootUID int, wantDir, wantMsg string) {
		t.Helper()
		err := checkDir(leaf, 0o700, uid, rootUID)
		if err == nil || !strings.Contains(err.Error(), wantDir) || !strings.Contains(err.Error(), wantMsg) {
			t.Fatalf("err=%v（%q と %q を含むはず）", err, wantDir, wantMsg)
		}
	}

	t.Run("親が自分所有の 0700 なら通る（根は root 所有と見なす）", func(t *testing.T) {
		_, leaf := setup(t, 0o700)
		// me を root と見なすと、最初の親（parent）で止まる。
		if err := checkDir(leaf, 0o700, me, me); err != nil {
			t.Fatal(err)
		}
		// 実際の uid でも、/tmp（root 所有 sticky）か root 所有の祖先で止まって通る。
		if err := CheckDir(leaf, 0o700, me); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("親が sticky 付き 0777 で root（か自分）所有（/tmp 相当）なら通る", func(t *testing.T) {
		_, leaf := setup(t, 0o777|os.ModeSticky)
		// leaf の持ち主を other、parent の持ち主を root と見なす（me を root にする）と、leaf 自身の持ち主検査が通らないので
		// 親だけを直接検査する。
		if err := checkAncestors(leaf, other, me); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("親が他人所有なら拒否し、どの親かをエラーに出す", func(t *testing.T) {
		parent, leaf := setup(t, 0o700)
		err := checkAncestors(leaf, other, -1)
		if err == nil || !strings.Contains(err.Error(), parent) || !strings.Contains(err.Error(), "持ち主") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("親が 0777 の非 sticky なら（自分所有でも root 所有でも）拒否", func(t *testing.T) {
		parent, leaf := setup(t, 0o777)
		reject(t, leaf, me, 0, parent, "書ける権限")
		reject(t, leaf, me, me, parent, "書ける権限")
	})
	t.Run("親が自分所有の 0775（グループ書き込み可）なら拒否", func(t *testing.T) {
		parent, leaf := setup(t, 0o775)
		reject(t, leaf, me, 0, parent, "書ける権限")
	})
	t.Run("親が他人所有の sticky 0777 は拒否（持ち主の検査が先）", func(t *testing.T) {
		parent, leaf := setup(t, 0o777|os.ModeSticky)
		err := checkAncestors(leaf, other, -1)
		if err == nil || !strings.Contains(err.Error(), parent) || !strings.Contains(err.Error(), "持ち主") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("もっと上の祖先が緩ければ拒否", func(t *testing.T) {
		top := filepath.Join(t.TempDir(), "top")
		mkdirMode(t, top, 0o777)
		mid := filepath.Join(top, "mid")
		mkdirMode(t, mid, 0o700)
		leaf := filepath.Join(mid, "leaf")
		mkdirMode(t, leaf, 0o700)
		reject(t, leaf, me, 0, top, "書ける権限")
	})
	t.Run("root 所有の親に届いたらそれより上は見ない", func(t *testing.T) {
		top := filepath.Join(t.TempDir(), "top")
		mkdirMode(t, top, 0o777) // 緩いが、mid を root と見なして止まるので見ない
		mid := filepath.Join(top, "mid")
		mkdirMode(t, mid, 0o755)
		leaf := filepath.Join(mid, "leaf")
		mkdirMode(t, leaf, 0o700)
		if err := checkAncestors(leaf, other, me); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("祖先のシンボリックリンクは解決した先を検査する（/tmp → /private/tmp 相当は通る）", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		mkdirMode(t, target, 0o700)
		mkdirMode(t, filepath.Join(target, "leaf"), 0o700)
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := CheckDir(filepath.Join(link, "leaf"), 0o700, me); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("祖先のリンクの解決先が他人のフォルダなら拒否", func(t *testing.T) {
		root := t.TempDir()
		evil := filepath.Join(root, "evil")
		mkdirMode(t, evil, 0o700)
		mkdirMode(t, filepath.Join(evil, "leaf"), 0o700)
		link := filepath.Join(root, "swarm-console")
		if err := os.Symlink(evil, link); err != nil {
			t.Fatal(err)
		}
		// 解決先 evil の持ち主を other と見なす（leaf の持ち主検査を避けるため祖先だけを検査する）。
		err := checkAncestors(filepath.Join(link, "leaf"), other, -1)
		if err == nil || !strings.Contains(err.Error(), evil) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("祖先を拒否したときは権限を直さない", func(t *testing.T) {
		parent, leaf := setup(t, 0o777)
		if err := os.Chmod(leaf, 0o750); err != nil { //nolint:gosec // 緩い権限を作るテスト
			t.Fatal(err)
		}
		reject(t, leaf, me, 0, parent, "書ける権限")
		if fi, _ := os.Stat(leaf); fi.Mode().Perm() != 0o750 {
			t.Fatalf("拒否したのに権限を変えました: %v", fi.Mode().Perm())
		}
	})
}
