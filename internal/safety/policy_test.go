package safety

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
)

type env struct {
	root, allowed, work, denied, deniedChild, outside, files string
	p                                                        *Policy
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, p, body string) string {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// newEnv は root/{allowed/{work,denied/child},outside,files} を作る。
// allowed が cwd の許可根、allowed/denied が拒否、files が file_roots、outside はどちらでもない。
func newEnv(t *testing.T) *env {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{root: root}
	e.allowed = mkdir(t, filepath.Join(root, "allowed"))
	e.work = mkdir(t, filepath.Join(e.allowed, "work"))
	e.denied = mkdir(t, filepath.Join(e.allowed, "denied"))
	e.deniedChild = mkdir(t, filepath.Join(e.denied, "child"))
	e.outside = mkdir(t, filepath.Join(root, "outside"))
	e.files = mkdir(t, filepath.Join(root, "files"))
	e.p = NewPolicy(&config.Config{
		AllowedRoots: []string{e.allowed},
		DeniedPaths:  []string{e.denied, filepath.Join(root, "not-yet-created")},
		FileRoots:    []string{e.files, e.allowed},
	})
	return e
}

func code(err error) apperr.Code {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	if err == nil {
		return ""
	}
	return "non-apperr"
}

func TestCheckCWD(t *testing.T) {
	e := newEnv(t)
	link := func(name, target string) string {
		p := filepath.Join(e.allowed, name)
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	escape := link("escape", e.outside)          // 許可内のリンク → 許可外
	toDenied := link("to-denied", e.deniedChild) // 許可内のリンク → 拒否の配下
	inside := link("inside", e.work)             // 許可内のリンク → 許可内
	dangling := link("dangling", e.root+"/nowhere")
	outLink := filepath.Join(e.outside, "into-allowed") // 許可外のリンク → 許可内
	if err := os.Symlink(e.work, outLink); err != nil {
		t.Fatal(err)
	}
	file := write(t, filepath.Join(e.work, "afile"), "x")
	sibling := mkdir(t, e.allowed+"2") // "allowed2" は "allowed" の配下ではない
	quote := mkdir(t, filepath.Join(e.work, "it's"))
	space := mkdir(t, filepath.Join(e.work, "with space 日本語"))

	tests := []struct {
		name, in string
		want     apperr.Code // "" は成功
		resolved string
	}{
		{"許可の根そのもの", e.allowed, "", e.allowed},
		{"許可の下", e.work, "", e.work},
		{"末尾スラッシュ・重複スラッシュ・.", e.work + "//./", "", e.work},
		{"空白・日本語", space, "", space},
		{"許可内のリンク → 許可内（解決後を返す）", inside, "", e.work},
		{"許可外のリンクから許可内へ", outLink, "", e.work},
		{".. で許可の下に戻る", e.work + "/../work", "", e.work},
		{".. で許可外へ抜ける", e.work + "/../../outside", apperr.ForbiddenPath, ""},
		{".. で根の外へ", e.allowed + "/../outside", apperr.ForbiddenPath, ""},
		{"許可外", e.outside, apperr.ForbiddenPath, ""},
		{"根の兄弟（接頭辞だけ一致）", sibling, apperr.ForbiddenPath, ""},
		{"/", "/", apperr.ForbiddenPath, ""},
		{"リンクで許可外へ抜ける", escape, apperr.ForbiddenPath, ""},
		{"リンクで許可外へ抜けた先の下", escape + "/", apperr.ForbiddenPath, ""},
		{"拒否のパスそのもの", e.denied, apperr.ForbiddenPath, ""},
		{"拒否の配下", e.deniedChild, apperr.ForbiddenPath, ""},
		{"拒否の配下へのリンク", toDenied, apperr.ForbiddenPath, ""},
		{"存在しない", filepath.Join(e.work, "missing"), apperr.ForbiddenPath, ""},
		{"壊れたリンク", dangling, apperr.ForbiddenPath, ""},
		{"ファイル", file, apperr.ForbiddenPath, ""},
		{"空", "", apperr.InvalidArgs, ""},
		{"相対", "work", apperr.InvalidArgs, ""},
		{"~", "~/work", apperr.InvalidArgs, ""},
		{"シングルクォート", quote, apperr.InvalidArgs, ""},
		{"改行", e.work + "\n", apperr.InvalidArgs, ""},
		{"CR", e.work + "\r", apperr.InvalidArgs, ""},
		{"NUL", e.work + "\x00", apperr.InvalidArgs, ""},
		{"ESC", e.work + "\x1b[0m", apperr.InvalidArgs, ""},
		{"タブ", e.work + "\t", apperr.InvalidArgs, ""},
		{"DEL", e.work + "\x7f", apperr.InvalidArgs, ""},
		{"C1", e.work + "\u009b", apperr.InvalidArgs, ""},
		{"; を含む（存在しない）", e.work + "; rm -rf /", apperr.ForbiddenPath, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.p.CheckCWD(tt.in)
			if code(err) != tt.want {
				t.Fatalf("CheckCWD(%q) = %q, %v; want code %q", tt.in, got, err, tt.want)
			}
			if tt.want == "" && got != tt.resolved {
				t.Fatalf("resolved = %q, want %q", got, tt.resolved)
			}
			if tt.want != "" && got != "" {
				t.Fatalf("エラーのとき値を返さない: %q", got)
			}
		})
	}
}

func TestCheckCWDErrorDetailsHaveNoPath(t *testing.T) {
	e := newEnv(t)
	_, err := e.p.CheckCWD(e.outside)
	ae := apperr.As(err)
	for k, v := range ae.Details {
		if s, ok := v.(string); ok && strings.Contains(s, e.outside) {
			t.Fatalf("details[%s] にパスが入っている: %v", k, v)
		}
	}
}

func TestCheckCWDEmptyPolicyDeniesAll(t *testing.T) {
	p := NewPolicy(&config.Config{})
	if _, err := p.CheckCWD(t.TempDir()); code(err) != apperr.ForbiddenPath {
		t.Fatalf("根が無ければ何も許可しない: %v", err)
	}
	if _, err := p.CheckFile("/etc/hostname"); code(err) != apperr.ForbiddenPath {
		t.Fatalf("根が無ければ何も許可しない: %v", err)
	}
}

func TestCheckCWDRootSlashAllowsEverythingExceptDenied(t *testing.T) {
	e := newEnv(t)
	p := NewPolicy(&config.Config{AllowedRoots: []string{"/"}, DeniedPaths: []string{e.denied}})
	if got, err := p.CheckCWD(e.outside); err != nil || got != e.outside {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := p.CheckCWD(e.deniedChild); code(err) != apperr.ForbiddenPath {
		t.Fatalf("%v", err)
	}
}

func TestCheckFileAndReadFile(t *testing.T) {
	e := newEnv(t)
	ok := write(t, filepath.Join(e.files, "ok.txt"), "こんにちは\n\tworld\n")
	inAllowed := write(t, filepath.Join(e.work, "in-allowed.txt"), "x")
	outside := write(t, filepath.Join(e.outside, "secret.txt"), "x")
	linkOut := filepath.Join(e.files, "link-out")
	if err := os.Symlink(outside, linkOut); err != nil {
		t.Fatal(err)
	}
	linkIn := filepath.Join(e.outside, "link-in")
	if err := os.Symlink(ok, linkIn); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(e.files, "dir-link")
	if err := os.Symlink(e.outside, dirLink); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(e.files, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, in string
		want     apperr.Code
	}{
		{"根の下", ok, ""},
		{"別の根の下", inAllowed, ""},
		{"許可外のリンクから根の下の実体へ", linkIn, ""},
		{".. を含む正当な経路", e.files + "/../files/ok.txt", ""},
		{"許可外", outside, apperr.ForbiddenPath},
		{"リンクで抜ける", linkOut, apperr.ForbiddenPath},
		{"ディレクトリのリンクで抜ける", dirLink + "/secret.txt", apperr.ForbiddenPath},
		{".. で抜ける", e.files + "/../outside/secret.txt", apperr.ForbiddenPath},
		{"許可外の存在しないファイル（存在を漏らさない）", filepath.Join(e.outside, "nope"), apperr.ForbiddenPath},
		{"根の下の存在しないファイル", filepath.Join(e.files, "nope"), apperr.FileUnreadable},
		{"ディレクトリ", e.files, apperr.FileUnreadable},
		{"FIFO", fifo, apperr.FileUnreadable},
		{"空", "", apperr.InvalidArgs},
		{"相対", "ok.txt", apperr.InvalidArgs},
		{"改行入り", ok + "\n", apperr.InvalidArgs},
		{"NUL 入り", ok + "\x00", apperr.InvalidArgs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.p.CheckFile(tt.in)
			if code(err) != tt.want {
				t.Fatalf("CheckFile(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
			if tt.want == "" && got != ok && got != inAllowed {
				t.Fatalf("解決後のパスのはず: %q", got)
			}
		})
	}

	if got, err := e.p.ReadFile(ok); err != nil || got != "こんにちは\n\tworld\n" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if _, err := e.p.ReadFile(outside); code(err) != apperr.ForbiddenPath {
		t.Fatalf("許可外: %v", err)
	}
	if _, err := e.p.ReadFile(fifo); code(err) != apperr.FileUnreadable {
		t.Fatalf("FIFO で固まらずにエラー: %v", err)
	}
}

func TestReadFileContentRules(t *testing.T) {
	e := newEnv(t)
	f := func(body string) string { return write(t, filepath.Join(e.files, "c.txt"), body) }
	tests := []struct {
		name string
		body string
		want apperr.Code
	}{
		{"空ファイル", "", ""},
		{"ちょうど 8000 字（日本語）", strings.Repeat("あ", 8000), ""},
		{"8001 字（日本語）", strings.Repeat("あ", 8001), apperr.TextTooLong},
		{"8001 字（ASCII）", strings.Repeat("a", 8001), apperr.TextTooLong},
		{"バイト数が大きい巨大ファイル", strings.Repeat("あ", 100000), apperr.TextTooLong},
		{"UTF-8 でない", "abc\xff\xfe", apperr.FileUnreadable},
		{"Latin-1", "caf\xe9", apperr.FileUnreadable},
		{"ESC を含む", "a\x1b[31mb", apperr.ForbiddenControlChar},
		{"CRLF", "a\r\nb", apperr.ForbiddenControlChar},
		{"NUL を含む", "a\x00b", apperr.ForbiddenControlChar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.p.ReadFile(f(tt.body))
			if code(err) != tt.want {
				t.Fatalf("code = %q (%v), want %q", code(err), err, tt.want)
			}
			if tt.want == "" && got != tt.body {
				t.Fatal("中身が違う")
			}
		})
	}
}

func TestReadFileUnreadablePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限で弾けない")
	}
	e := newEnv(t)
	p := write(t, filepath.Join(e.files, "noperm.txt"), "x")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.ReadFile(p); code(err) != apperr.FileUnreadable {
		t.Fatalf("%v", err)
	}
}
