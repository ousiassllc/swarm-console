package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

func TestTokenListEmptyAndAllSessions(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	var out, errb strings.Builder
	if err := TokenList(cfg, &out, &errb); err != nil || out.Len() != 0 || errb.Len() == 0 {
		t.Fatalf("トークンなし: err=%v stdout=%q stderr=%q", err, out.String(), errb.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg), "tokens.toml")); err == nil {
		t.Fatal("一覧を見ただけで tokens.toml ができています")
	}
	if err := TokenAdd(cfg, "ops1", auth.ScopeOps, nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	plain := strings.TrimSpace(out.String())
	out.Reset()
	if err := TokenList(cfg, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if f := strings.Fields(out.String()); len(f) != 5 || f[0] != "ops1" || f[1] != "ops" || f[2] != "all" || f[4] != "active" {
		t.Errorf("token list = %q", out.String())
	}
	if strings.Contains(out.String(), plain) || strings.Contains(out.String(), "sha256") {
		t.Error("token list に平文かハッシュが出ています")
	}
}

func TestParseSessions(t *testing.T) {
	for name, c := range map[string]struct {
		in   string
		want []string // nil なら拒否
	}{
		"1 つ":      {"0", []string{"0"}},
		"複数":       {"0,1", []string{"0", "1"}},
		"前後の空白を除く": {" 0 , work ,x ", []string{"0", "work", "x"}},
		"空文字":      {"", nil},
		"空の要素（途中）": {"0,,1", nil},
		"空の要素（末尾）": {"0,", nil},
		"空の要素（先頭）": {",0", nil},
		"空白だけの要素":  {"0, ,1", nil},
		"重複は検査しない": {"0,0", []string{"0", "0"}},
	} {
		got, err := ParseSessions(c.in)
		if c.want == nil {
			if err == nil {
				t.Errorf("%s: %q を拒否しなかった（%v）", name, c.in, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ParseSessions(%q) = %v, %v, want %v", name, c.in, got, err, c.want)
		}
	}
}
