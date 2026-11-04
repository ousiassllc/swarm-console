package filegettests_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/safety"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

func TestFileGetRejectsOutsideAllowedRoots(t *testing.T) {
	e := newFileGetEnv(t)
	outside := writeFile(t, filepath.Join(e.outside, "shot.png"), pngBytes)
	// 許可の外は、存在しなくても path_forbidden（存在の有無を漏らさない）。
	for _, p := range []string{outside, filepath.Join(e.outside, "none.png"), "/etc/passwd", "/", e.Root + "/x.png"} {
		_, err := fileGet(t, e.Fixture, p)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
	}
	// 相対パスは断る。
	_, err := fileGet(t, e.Fixture, "shot.png")
	wantCode(t, err, apperr.PathForbidden)
}

// 根の文字列接頭辞が同じだけの別ディレクトリ（/a/b と /a/bc）は許可しない。
func TestFileGetRejectsSiblingWithSamePrefix(t *testing.T) {
	e := newFileGetEnv(t)
	sibling := writeFile(t, e.work+"x/shot.png", pngBytes)
	t.Cleanup(func() { _ = os.RemoveAll(e.work + "x") })
	_, err := fileGet(t, e.Fixture, sibling)
	wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
}

func TestFileGetRejectsDotDot(t *testing.T) {
	e := newFileGetEnv(t)
	writeFile(t, filepath.Join(e.outside, "shot.png"), pngBytes)
	writeFile(t, filepath.Join(e.work, "in.png"), pngBytes)
	rel, err := filepath.Rel(e.work, e.outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		e.work + "/../" + filepath.Base(e.outside) + "/shot.png",
		e.work + "/" + rel + "/shot.png",
		e.work + "/sub/../../" + filepath.Base(e.outside) + "/shot.png",
		e.work + "/../" + filepath.Base(e.work) + "/in.png", // 中に戻ってくるパスも「..」を含めば断る
		e.work + "/..",
	} {
		_, err := fileGet(t, e.Fixture, p)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "dotdot")
	}
}

func TestFileGetSymlinks(t *testing.T) {
	e := newFileGetEnv(t)
	secretOutside := writeFile(t, filepath.Join(e.outside, "shot.png"), pngBytes)
	inside := writeFile(t, filepath.Join(e.work, "real.png"), pngBytes)

	// 許可リストの外を指すリンクは、ファイルでもディレクトリでも断る。
	fileLink := symlink(t, secretOutside, filepath.Join(e.work, "link.png"))
	dirLink := symlink(t, e.outside, filepath.Join(e.work, "dirlink"))
	for _, p := range []string{fileLink, filepath.Join(dirLink, "shot.png"), filepath.Join(dirLink, "none.png")} {
		_, err := fileGet(t, e.Fixture, p)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "outside_roots")
	}

	// 許可リストの中で内側を指すリンクは通り、path は実パスで返る。
	okLink := symlink(t, inside, filepath.Join(e.work, "inner.png"))
	got, err := fileGet(t, e.Fixture, okLink)
	if err != nil || got["path"] != inside {
		t.Fatalf("内側へのリンク = %v, %v", got, err)
	}

	// ルート自体がシンボリックリンクでも動く（cwd がリンク経由・実体の両方から読める）。
	rootLink := symlink(t, e.work, filepath.Join(realTempDir(t), "cwdlink"))
	e.Registry.Snap.Panes[1].Cwd = rootLink
	for _, p := range []string{filepath.Join(rootLink, "real.png"), inside} {
		if got, err := fileGet(t, e.Fixture, p); err != nil || got["path"] != inside {
			t.Errorf("%s = %v, %v", p, got, err)
		}
	}
}

func TestFileGetRejectsSecretFiles(t *testing.T) {
	e := newFileGetEnv(t)
	names := []string{
		".env", ".env.local", ".env.production.png", "server.pem", "server.key", "id_rsa", "id_ed25519", "id_rsa.png",
		"tokens.toml", "config.toml", "profiles.toml", "TOKENS.TOML", ".ENV", "ID_RSA",
	}
	for _, name := range names {
		path := writeFile(t, filepath.Join(e.work, name), pngBytes) // 許可リストの中でも、中身が画像でも断る
		_, err := fileGet(t, e.Fixture, path)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "secret")
	}

	// 名前が無害でも、実体が秘密のリンク（画像拡張子つき）は断る。
	for _, name := range []string{"secret.key", "id_rsa", ".env", "tokens.toml"} {
		target := writeFile(t, filepath.Join(e.work, "store", name), pngBytes)
		link := symlink(t, target, filepath.Join(e.work, "harmless-"+name+".png"))
		_, err := fileGet(t, e.Fixture, link)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "secret")
	}

	// tokens・notes の実パスは、名前が普通でも（許可リストの中でも）断る。リンク経由でも同じ。
	tokens := filepath.Join(e.work, "tok.png")
	notes := filepath.Join(e.work, "notes.png")
	e.Cfg.TokensPath, e.Cfg.NotesPath = tokens, notes
	e.Deps.Safety = safety.NewPolicy(e.Cfg)
	var err error
	if e.Tools, err = tools.New(e.Deps); err != nil {
		t.Fatal(err)
	}
	writeFile(t, tokens, pngBytes)
	writeFile(t, notes, pngBytes)
	for _, p := range []string{tokens, notes, symlink(t, notes, filepath.Join(e.work, "via-link.png"))} {
		_, err := fileGet(t, e.Fixture, p)
		wantReason(t, wantCode(t, err, apperr.PathForbidden), "secret")
	}
}

func TestFileGetRejectsDeniedPaths(t *testing.T) {
	e := newFileGetEnv(t)
	path := writeFile(t, filepath.Join(e.work, "denied", "shot.png"), pngBytes)
	// Policy は作った時点の設定を写すので、denied_paths を変えたら作り直す。
	e.Cfg.DeniedPaths = []string{filepath.Join(e.work, "denied")}
	e.Deps.Safety = safety.NewPolicy(e.Cfg)
	var err error
	if e.Tools, err = tools.New(e.Deps); err != nil {
		t.Fatal(err)
	}
	_, err = fileGet(t, e.Fixture, path)
	wantReason(t, wantCode(t, err, apperr.PathForbidden), "denied")
}
