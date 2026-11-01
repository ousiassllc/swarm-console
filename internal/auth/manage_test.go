package auth

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// go-toml は引用符の無い安全な文字列をリテラル文字列（単引用符）で書く。TOML として等価。
func TestAddWritesHashOnlyWith600AndCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "conf")
	path := filepath.Join(dir, "tokens.toml")
	now := time.Date(2026, 10, 31, 22, 5, 0, 0, time.FixedZone("JST", 9*3600))
	tok, err := Add(path, "ops-client_1", ScopeOps, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "sc_") {
		t.Fatalf("接頭辞が sc_ でない")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tok, "sc_"))
	if err != nil || len(raw) != 32 {
		t.Fatalf("base64url 32 バイトでない: len=%d err=%v", len(raw), err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("ファイル権限 = %v err=%v", fi.Mode().Perm(), err)
	}
	di, err := os.Stat(dir)
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("ディレクトリ権限 = %v err=%v", di.Mode().Perm(), err)
	}
	body := readFile(t, path)
	if strings.Contains(body, tok) || strings.Contains(body, strings.TrimPrefix(tok, "sc_")) {
		t.Fatal("平文がファイルに含まれる")
	}
	if !regexp.MustCompile(`(?m)^hash = ['"]sha256:[0-9a-f]{64}['"]$`).MatchString(body) {
		t.Fatalf("hash の形式が違う:\n%s", body)
	}
	if !strings.Contains(body, `created_at = '2026-10-31T22:05:00+09:00'`) {
		t.Fatalf("created_at が now のオフセットのままでない:\n%s", body)
	}
	if strings.Contains(body, "revoked_at") {
		t.Fatal("失効していないのに revoked_at がある")
	}
	order := []string{"name =", "scope =", "hash =", "created_at ="}
	last := -1
	for _, k := range order {
		i := strings.Index(body, k)
		if i <= last {
			t.Fatalf("キー順が name, scope, hash, created_at でない:\n%s", body)
		}
		last = i
	}
}

func TestAddedPlaintextAuthenticates(t *testing.T) {
	f := newFixture(t)
	if c, err := f.a.Authenticate(f.bearer("adm")); err != nil || c.Scope != ScopeOps {
		t.Fatalf("c=%+v err=%v", c, err)
	}
}

func TestAddAppendsKeepingExistingEntries(t *testing.T) {
	f := newFixture(t)
	f.add(t, "third", ScopeReport)
	if n := strings.Count(readFile(t, f.path), "[[token]]"); n != 3 {
		t.Fatalf("[[token]] = %d", n)
	}
	f.open(t)
	for _, name := range []string{"rep", "adm", "third"} {
		if _, err := f.a.Authenticate(f.bearer(name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestAddRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	if err := Revoke(f.path, "rep", f.clk.Now()); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, f.path)
	cases := map[string]struct {
		name  string
		scope Scope
	}{
		"重複":        {"adm", ScopeOps},
		"失効済み名の再利用": {"rep", ScopeReport},
		"大文字":       {"Abc", ScopeOps},
		"空":         {"", ScopeOps},
		"33 文字":     {strings.Repeat("a", 33), ScopeOps},
		"先頭 -":      {"-abc", ScopeOps},
		"未知の scope": {"fresh", "admin"},
		"空の scope":  {"fresh", ""},
		"スペースを含む名前": {"a b", ScopeOps},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			if tok, err := Add(f.path, c.name, c.scope, f.clk.Now()); err == nil || tok != "" {
				t.Fatalf("拒否されるはず: tok=%q err=%v", tok, err)
			}
			if readFile(t, f.path) != before {
				t.Fatal("拒否したのにファイルが変わった")
			}
		})
	}
	// 境界: 32 文字は通る。
	if _, err := Add(f.path, strings.Repeat("a", 32), ScopeOps, f.clk.Now()); err != nil {
		t.Fatalf("32 文字: %v", err)
	}
}

func TestAddAndRevokeRefuseBadExistingFile(t *testing.T) {
	f := newFixture(t)
	chmod(t, f.path, 0o644)
	if _, err := Add(f.path, "other", ScopeOps, f.clk.Now()); err == nil {
		t.Fatal("権限 644 のファイルへ Add できてしまう")
	}
	if err := Revoke(f.path, "rep", f.clk.Now()); err == nil {
		t.Fatal("権限 644 のファイルへ Revoke できてしまう")
	}
	chmod(t, f.path, 0o600)
	writeRaw(t, f.path, "[[token]\n", 0o600)
	if _, err := Add(f.path, "other", ScopeOps, f.clk.Now()); err == nil {
		t.Fatal("壊れた TOML へ Add できてしまう")
	}
}

func TestRevoke(t *testing.T) {
	f := newFixture(t)
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.FixedZone("JST", 9*3600))
	if err := Revoke(f.path, "rep", at); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, f.path)
	if !strings.Contains(body, `revoked_at = '2026-11-02T09:00:00+09:00'`) {
		t.Fatalf("revoked_at が無い:\n%s", body)
	}
	if strings.Count(body, "revoked_at") != 1 {
		t.Fatal("他のトークンまで失効している")
	}
	f.open(t)
	_, err := f.a.Authenticate(f.bearer("rep"))
	requireUnauthorized(t, err)
	if _, err := f.a.Authenticate(f.bearer("adm")); err != nil {
		t.Fatalf("他のトークンは生きている: %v", err)
	}
	// 二重失効は元の revoked_at を上書きせずエラー。
	if err := Revoke(f.path, "rep", at.Add(time.Hour)); err == nil {
		t.Fatal("二重失効はエラーのはず")
	}
	if readFile(t, f.path) != body {
		t.Fatal("二重失効でファイルが変わった")
	}
}

func TestRevokeUnknownNameIsError(t *testing.T) {
	f := newFixture(t)
	err := Revoke(f.path, "nobody", f.clk.Now())
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Fatalf("err = %v", err)
	}
	if err := Revoke(filepath.Join(t.TempDir(), "none.toml"), "x", f.clk.Now()); err == nil {
		t.Fatal("ファイル無しの Revoke はエラーのはず")
	}
}
