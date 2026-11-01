package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

const (
	okHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	okRec  = "[[token]]\nname = \"a\"\nscope = \"report\"\nhash = \"" + okHash + "\"\ncreated_at = \"2026-10-31T22:00:00+09:00\"\n"
)

func TestOpenRejectsBadFiles(t *testing.T) {
	cases := map[string]struct {
		content string
		mode    os.FileMode
		wantErr string // 空でなければエラー文字列に含まれること
	}{
		"権限 644":     {okRec, 0o644, "権限"},
		"権限 700":     {okRec, 0o700, "権限"},
		"壊れた TOML":   {"[[token]\nname = ", 0o600, "TOML"},
		"未知の scope":  {strings.Replace(okRec, "report", "admin", 1), 0o600, "scope"},
		"name 大文字":   {strings.Replace(okRec, `"a"`, `"Abc"`, 1), 0o600, "name"},
		"name 先頭 -":  {strings.Replace(okRec, `"a"`, `"-a"`, 1), 0o600, "name"},
		"name 空":     {strings.Replace(okRec, `"a"`, `""`, 1), 0o600, "name"},
		"hash 短い":    {strings.Replace(okRec, okHash, "sha256:abcd", 1), 0o600, "hash"},
		"hash 接頭辞違い": {strings.Replace(okRec, "sha256:", "md5:", 1), 0o600, "hash"},
		"hash 大文字":   {strings.Replace(okRec, "0000", "ABCD", 1), 0o600, "hash"},
		"name 重複":    {okRec + okRec, 0o600, "重複"},
		"未知のキー":      {okRec + "token_plain = \"x\"\n", 0o600, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tokens.toml")
			writeRaw(t, path, c.content, c.mode)
			_, err := Open(path, clock.NewFake(baseTime))
			if err == nil {
				t.Fatal("起動拒否（error）になるはず")
			}
			if c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q を含む", err, c.wantErr)
			}
			if strings.Contains(err.Error(), okHash) {
				t.Fatalf("エラーにハッシュが含まれる: %v", err)
			}
		})
	}
}

func TestOpenAcceptsValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.toml")
	writeRaw(t, path, okRec, 0o600)
	if _, err := Open(path, clock.NewFake(baseTime)); err != nil {
		t.Fatal(err)
	}
}
