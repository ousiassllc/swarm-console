package filegettests_test

import (
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// cwdHomeEnv は HOME を一時ディレクトリ home に差し替えた newFileGetEnv（実ホームを使わない）。
func cwdHomeEnv(t *testing.T) (e *fileGetEnv, home string) {
	t.Helper()
	home = realTempDir(t)
	t.Setenv("HOME", home)
	return newFileGetEnv(t), home
}

// ペインの cwd が / やホーム（実パスで一致するもの。シンボリックリンク経由を含む）のときは許可の根にしない。
func TestFileGetPaneCwdBroadIsNotRoot(t *testing.T) {
	tests := []struct {
		name string
		cwd  func(t *testing.T, e *fileGetEnv, home string) string
		pic  func(e *fileGetEnv, home string) string // 取ろうとする画像（許可リストの外）
	}{
		{"cwd が /", func(*testing.T, *fileGetEnv, string) string { return "/" },
			func(e *fileGetEnv, _ string) string { return filepath.Join(e.outside, "shot.png") }},
		{"cwd がホーム", func(_ *testing.T, _ *fileGetEnv, home string) string { return home },
			func(_ *fileGetEnv, home string) string { return filepath.Join(home, "pics", "shot.png") }},
		{"cwd が / を指すシンボリックリンク", func(t *testing.T, e *fileGetEnv, _ string) string {
			return symlink(t, "/", filepath.Join(e.outside, "root-link"))
		}, func(e *fileGetEnv, _ string) string { return filepath.Join(e.outside, "shot.png") }},
		{"cwd がホームを指すシンボリックリンク", func(t *testing.T, e *fileGetEnv, home string) string {
			return symlink(t, home, filepath.Join(e.outside, "home-link"))
		}, func(_ *fileGetEnv, home string) string { return filepath.Join(home, "pics", "shot.png") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, home := cwdHomeEnv(t)
			e.Registry.Snap.Panes[0].Cwd = tt.cwd(t, e, home)
			pic := writeFile(t, tt.pic(e, home), pngBytes)
			_, err := fileGet(t, e.Fixture, pic)
			wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
		})
	}
}

// ホームの下の普通の作業フォルダが cwd なら、その下は取れる。
func TestFileGetPaneCwdUnderHomeIsRoot(t *testing.T) {
	e, home := cwdHomeEnv(t)
	cwd := filepath.Join(home, "projects", "x")
	writeFile(t, filepath.Join(cwd, "keep.txt"), []byte("x")) // cwd を作る
	e.Registry.Snap.Panes[0].Cwd = cwd
	pic := writeFile(t, filepath.Join(cwd, "shot.png"), pngBytes)
	if _, err := fileGet(t, e.Fixture, pic); err != nil {
		t.Fatal(err)
	}
}

// cwd が / やホームでも、明示の根（file_get_roots・レポート置き場）の下は取れる。
func TestFileGetPaneCwdBroadKeepsExplicitRoots(t *testing.T) {
	t.Run("file_get_roots に明示したフォルダ", func(t *testing.T) {
		e, home := cwdHomeEnv(t)
		e.Registry.Snap.Panes[0].Cwd = home
		extra := filepath.Join(home, "shots")
		e.Cfg.FileGetRoots = []string{extra}
		pic := writeFile(t, filepath.Join(extra, "shot.png"), pngBytes)
		if _, err := fileGet(t, e.Fixture, pic); err != nil {
			t.Fatal(err)
		}
		// 明示の根の外（ホーム直下の別フォルダ）は取れない。
		other := writeFile(t, filepath.Join(home, "other", "shot.png"), pngBytes)
		_, err := fileGet(t, e.Fixture, other)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
	})
	t.Run("file_get_roots に / を明示", func(t *testing.T) {
		e, _ := cwdHomeEnv(t)
		e.Registry.Snap.Panes[0].Cwd = "/"
		e.Cfg.FileGetRoots = []string{"/"}
		pic := writeFile(t, filepath.Join(e.outside, "shot.png"), pngBytes)
		if _, err := fileGet(t, e.Fixture, pic); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("file_get_roots にホームを明示", func(t *testing.T) {
		e, home := cwdHomeEnv(t)
		e.Registry.Snap.Panes[0].Cwd = home
		e.Cfg.FileGetRoots = []string{home}
		pic := writeFile(t, filepath.Join(home, "pics", "shot.png"), pngBytes)
		if _, err := fileGet(t, e.Fixture, pic); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("レポート置き場", func(t *testing.T) {
		e, home := cwdHomeEnv(t)
		e.Registry.Snap.Panes[0].Cwd = home
		pic := writeFile(t, filepath.Join(e.Cfg.ReportDir, "T1", "shot.png"), pngBytes)
		if _, err := fileGet(t, e.Fixture, pic); err != nil {
			t.Fatal(err)
		}
	})
}
