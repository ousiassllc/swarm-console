package tooltests_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/safety"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// 先頭バイトが各形式の最小の中身。本物の画像・動画でなくても、先頭で形式を見分ける。
var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), []byte("png-body")...)
	jpgBytes  = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("jpeg-body")...)
	webpBytes = append([]byte("RIFF\x10\x00\x00\x00WEBP"), []byte("VP8 body")...)
	gifBytes  = append([]byte("GIF89a"), []byte("gif-body")...)
	mp4Bytes  = append([]byte("\x00\x00\x00\x18ftypmp42"), []byte("mp4-body")...)
)

// fileGetFixture は 1 つ目のペインの cwd を一時ディレクトリ work にした Fixture（work は根の外の別ディレクトリ）。
type fileGetEnv struct {
	*toolstest.Fixture
	work, outside string
}

func newFileGetEnv(t *testing.T) *fileGetEnv {
	t.Helper()
	f := toolstest.New(t)
	e := &fileGetEnv{Fixture: f, work: realTempDir(t), outside: realTempDir(t)}
	f.Registry.Snap.Panes[0].Cwd = e.work
	return e
}

func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func symlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func fileGet(t *testing.T, f *toolstest.Fixture, path string) (map[string]any, error) {
	t.Helper()
	text, err := call(t, f, "file_get", `{"path":`+quote(path)+`}`)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("file_get の返り値が JSON ではありません: %q", text)
	}
	return m, nil
}

func quote(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }

func wantReason(t *testing.T, d map[string]any, reason string) {
	t.Helper()
	if d["reason"] != reason {
		t.Errorf("details.reason = %v, want %s", d["reason"], reason)
	}
}

func TestFileGetReturnsMediaWithMatchingSHA256(t *testing.T) {
	tests := []struct {
		name string
		mime string
		data []byte
	}{
		{"shot.png", "image/png", pngBytes},
		{"shot.jpg", "image/jpeg", jpgBytes},
		{"shot.jpeg", "image/jpeg", jpgBytes},
		{"shot.webp", "image/webp", webpBytes},
		{"shot.gif", "image/gif", gifBytes},
		{"clip.mp4", "video/mp4", mp4Bytes},
		{"UPPER.PNG", "image/png", pngBytes}, // 拡張子の大文字小文字は区別しない
		{"sub/dir/clip.MP4", "video/mp4", mp4Bytes},
	}
	e := newFileGetEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(e.work, tt.name), tt.data)
			got, err := fileGet(t, e.Fixture, path)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(tt.data) // ファイルを直接ハッシュした値
			disk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if sum != sha256.Sum256(disk) {
				t.Fatal("テストの前提が崩れています")
			}
			if got["sha256"] != hex.EncodeToString(sum[:]) || got["mime"] != tt.mime ||
				got["size"] != float64(len(tt.data)) || got["path"] != path {
				t.Errorf("結果 = %v", got)
			}
			dec, err := base64.StdEncoding.DecodeString(got["data_base64"].(string))
			if err != nil || string(dec) != string(tt.data) {
				t.Errorf("data_base64 が元のバイト列と一致しません: %v", err)
			}
		})
	}
}

// 出力のキー順は path・mime・size・sha256・data_base64 に固定。
func TestFileGetKeyOrder(t *testing.T) {
	e := newFileGetEnv(t)
	path := writeFile(t, filepath.Join(e.work, "a.png"), pngBytes)
	text, err := call(t, e.Fixture, "file_get", `{"path":`+quote(path)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"path":` + quote(path) + `,"mime":"image/png","size":` + "16" + `,"sha256":"`
	if !strings.HasPrefix(text, want) || !strings.Contains(text, `","data_base64":"`) {
		t.Errorf("本文 = %s", text)
	}
}

// 許可リストの根は、ペインの cwd・レポート置き場・設定の file_get_roots。ペインの cwd は許可されたセッションのものだけ。
func TestFileGetAllowedRoots(t *testing.T) {
	e := newFileGetEnv(t)
	extra := realTempDir(t)
	e.Cfg.FileGetRoots = []string{extra}
	for name, path := range map[string]string{
		"レポート置き場":        filepath.Join(e.Cfg.ReportDir, "T1", "shot.png"),
		"file_get_roots": filepath.Join(extra, "shot.png"),
		"2 つ目のペインの cwd":  filepath.Join(e.outside, "shot.png"),
	} {
		writeFile(t, path, pngBytes)
		if name == "2 つ目のペインの cwd" {
			e.Registry.Snap.Panes[1].Cwd = e.outside
		}
		if _, err := fileGet(t, e.Fixture, path); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

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

func TestFileGetTooLarge(t *testing.T) {
	e := newFileGetEnv(t)
	e.Cfg.FileGetMaxBytes = 64
	exact := writeFile(t, filepath.Join(e.work, "exact.png"), append(append([]byte{}, pngBytes...), make([]byte, 64-len(pngBytes))...))
	over := writeFile(t, filepath.Join(e.work, "over.png"), append(append([]byte{}, pngBytes...), make([]byte, 65-len(pngBytes))...))
	if got, err := fileGet(t, e.Fixture, exact); err != nil || got["size"] != float64(64) {
		t.Fatalf("ちょうど上限 = %v, %v", got, err)
	}
	d := wantCode(t, mustErr(fileGet(t, e.Fixture, over)), apperr.TooLarge)
	if d["max"] != int64(64) {
		t.Errorf("details = %v", d)
	}
	// 上限を上げれば通る（config の値が効く）。
	e.Cfg.FileGetMaxBytes = 65
	if _, err := fileGet(t, e.Fixture, over); err != nil {
		t.Errorf("上限 65 で 65 バイト = %v", err)
	}
}

func mustErr(_ map[string]any, err error) error { return err }

func TestFileGetRejectsUnsupportedType(t *testing.T) {
	e := newFileGetEnv(t)
	tests := []struct {
		name   string
		data   []byte
		reason string
	}{
		{"notes.txt", []byte("hello"), "extension"},
		{"vector.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), "extension"},
		{"clip.mov", mp4Bytes, "extension"},
		{"noext", pngBytes, "extension"},
		{"fake.png", []byte("これは画像ではありません"), "content"},
		{"empty.png", nil, "content"},
		{"short.gif", []byte("GIF"), "content"},
		{"wrong.jpg", pngBytes, "content"}, // 拡張子と中身の形式が合わない
		{"wrong.mp4", gifBytes, "content"},
		{"riff.webp", []byte("RIFF\x10\x00\x00\x00WAVEfmt "), "content"},
		{"html.png", []byte("<html><script>alert(1)</script></html>"), "content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(e.work, tt.name), tt.data)
			_, err := fileGet(t, e.Fixture, path)
			wantReason(t, wantCode(t, err, apperr.UnsupportedType), tt.reason)
		})
	}
}

func TestFileGetRejectsNonRegularFiles(t *testing.T) {
	e := newFileGetEnv(t)
	dir := filepath.Join(e.work, "dir.png")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(e.work, "pipe.png")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo できません: %v", err)
	}
	for _, p := range []string{dir, fifo} {
		_, err := fileGet(t, e.Fixture, p) // FIFO を開いて固まらない
		wantReason(t, wantCode(t, err, apperr.UnsupportedType), "not_regular")
	}
}

func TestFileGetNotFound(t *testing.T) {
	e := newFileGetEnv(t)
	for _, p := range []string{
		filepath.Join(e.work, "none.png"),
		filepath.Join(e.work, "no", "such", "dir", "none.png"),
		filepath.Join(e.Cfg.ReportDir, "none.png"),
	} {
		_, err := fileGet(t, e.Fixture, p)
		wantCode(t, err, apperr.NotFound)
	}
}

func TestFileGetInvalidArgs(t *testing.T) {
	e := newFileGetEnv(t)
	for _, args := range []string{`{}`, `{"path":""}`, `{"path":"/x\u0000.png"}`, `{"path":"a","extra":1}`, `{"path":1}`, `{"Path":"/a.png"}`, `[]`} {
		_, err := call(t, e.Fixture, "file_get", args)
		wantCode(t, err, apperr.InvalidArgs)
	}
}

// 触れないセッションのペインの cwd は許可リストに入らない。
func TestFileGetIgnoresCwdOfForbiddenSessions(t *testing.T) {
	e := newFileGetEnv(t)
	e.UseTwoSessions()
	last := len(e.Registry.Snap.Panes) - 1
	e.Registry.Snap.Panes[last].Cwd = e.outside
	path := writeFile(t, filepath.Join(e.outside, "shot.png"), pngBytes)
	tool, ok := e.Tools.Lookup(tools.EndpointOps, "file_get")
	if !ok {
		t.Fatal("file_get がありません")
	}
	args := json.RawMessage(`{"path":` + quote(path) + `}`)
	if _, err := tool.Handler(context.Background(), auth.Client{Name: "s1", Scope: auth.ScopeOps, AllowedSessions: []string{"0"}}, args); err == nil {
		t.Error("セッション 1 の cwd が許可されました")
	} else {
		wantCode(t, err, apperr.PathForbidden)
	}
	if _, err := tool.Handler(context.Background(), auth.Client{Name: "s2", Scope: auth.ScopeOps, AllowedSessions: []string{"1"}}, args); err != nil {
		t.Errorf("セッション 1 のトークンが自分のペインの cwd を読めません: %v", err)
	}
}

// 名前の無い（認証を通っていない）Client は何も許可されず、ペインの cwd の根も持たない。
func TestFileGetRequiresAuthenticatedClientForPaneRoots(t *testing.T) {
	e := newFileGetEnv(t)
	path := writeFile(t, filepath.Join(e.work, "shot.png"), pngBytes)
	tool, _ := e.Tools.Lookup(tools.EndpointOps, "file_get")
	_, err := tool.Handler(context.Background(), auth.Client{}, json.RawMessage(`{"path":`+quote(path)+`}`))
	wantCode(t, err, apperr.PathForbidden)
}
