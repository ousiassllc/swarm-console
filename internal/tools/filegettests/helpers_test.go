package filegettests_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
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
			sum := sha256.Sum256(tt.data)  // ファイルを直接ハッシュした値
			disk, err := os.ReadFile(path) //nolint:gosec // テストが作った一時ファイル
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

var testClient = auth.Client{Name: "tester", Scope: auth.ScopeOps}

// call は /ops に出ている file_get を args で呼ぶ。
func call(t *testing.T, f *toolstest.Fixture, name, args string) (string, error) {
	t.Helper()
	tool, ok := f.Tools.Lookup(toolkit.EndpointOps, name)
	if !ok {
		t.Fatalf("ツール %s がありません", name)
	}
	res, err := tool.Handler(context.Background(), testClient, json.RawMessage(args))
	return res.Text, err
}

// wantCode は err が code の *apperr.Error であることを確かめ、details を返す。
func wantCode(t *testing.T, err error, code apperr.Code) map[string]any {
	t.Helper()
	if err == nil {
		t.Fatalf("エラー %s を期待しましたが成功しました", code)
	}
	e := apperr.As(err)
	if e.Code != code {
		t.Fatalf("エラーコード = %s, want %s (%v)", e.Code, code, err)
	}
	return e.Details
}
