package filegettests_test

import (
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// 許可の中にある、宛先が存在しないリンク。宛先が外なら、外のパスと同じく outside_roots（宛先の有無で結果を変えない）。
// 宛先が許可の中なら not_found。
func TestFileGetDanglingSymlinks(t *testing.T) {
	e := newFileGetEnv(t)
	existing := writeFile(t, filepath.Join(e.outside, "exists.png"), pngBytes)
	absent := filepath.Join(e.outside, "absent.png")
	rel, err := filepath.Rel(e.work, absent)
	if err != nil {
		t.Fatal(err)
	}

	outsideCases := map[string]string{
		"外・宛先あり":        symlink(t, existing, filepath.Join(e.work, "to-existing.png")),
		"外・宛先なし":        symlink(t, absent, filepath.Join(e.work, "to-absent.png")),
		"外・宛先なし（相対）":    symlink(t, rel, filepath.Join(e.work, "to-absent-rel.png")),
		"外・宛先なし・2 段":    symlink(t, filepath.Join(e.work, "to-absent.png"), filepath.Join(e.work, "chain.png")),
		"外のディレクトリの下・なし": symlink(t, filepath.Join(e.outside, "nodir", "x.png"), filepath.Join(e.work, "to-nodir.png")),
	}
	for name, link := range outsideCases {
		t.Run(name, func(t *testing.T) {
			_, err := fileGet(t, e.Fixture, link)
			wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
		})
	}

	// 宛先が許可の中で、まだ無いものは not_found（絶対・相対・2 段）。
	insideAbsent := filepath.Join(e.work, "absent.png")
	for name, link := range map[string]string{
		"中・宛先なし":      symlink(t, insideAbsent, filepath.Join(e.work, "in-absent.png")),
		"中・宛先なし（相対）":  symlink(t, "absent.png", filepath.Join(e.work, "in-absent-rel.png")),
		"中・宛先なし・2 段":  symlink(t, filepath.Join(e.work, "in-absent.png"), filepath.Join(e.work, "in-chain.png")),
		"中・宛先なしの下の成分": symlink(t, filepath.Join(e.work, "nodir"), filepath.Join(e.work, "in-dir")) + "/x.png",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fileGet(t, e.Fixture, link)
			wantCode(t, err, apperr.NotFound)
		})
	}
}

// リンクの循環は、存在しない外のパスと区別を付けず outside_roots で断る。
func TestFileGetSymlinkLoop(t *testing.T) {
	e := newFileGetEnv(t)
	a, b := filepath.Join(e.work, "a.png"), filepath.Join(e.work, "b.png")
	symlink(t, b, a)
	symlink(t, a, b)
	self := symlink(t, filepath.Join(e.work, "self.png"), filepath.Join(e.work, "self.png"))
	for _, p := range []string{a, b, self, filepath.Join(a, "x.png")} {
		_, err := fileGet(t, e.Fixture, p)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
	}
	// 外のディレクトリの中の循環を、許可の中のリンクから指しても同じ。
	x, y := filepath.Join(e.outside, "x.png"), filepath.Join(e.outside, "y.png")
	symlink(t, y, x)
	symlink(t, x, y)
	_, err := fileGet(t, e.Fixture, symlink(t, x, filepath.Join(e.work, "to-loop.png")))
	wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
}
