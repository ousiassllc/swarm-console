package app

import (
	"os"
	"path/filepath"
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
