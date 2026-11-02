package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

var tokenRe = regexp.MustCompile(`^sc_[A-Za-z0-9_-]{43}$`)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestTokenAddWritesHashOnlyAndPrintsPlaintextOnce(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	code, stdout, stderr := runCLI(t, "token", "add", "agent", "--scope", "ops", "--config", cfg)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	plain := strings.TrimSpace(stdout)
	if !tokenRe.MatchString(plain) || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("標準出力 = %q（sc_ + 43 字の 1 行だけのはず）", stdout)
	}
	if strings.Contains(stderr, plain) {
		t.Error("標準エラーにも平文が出ています")
	}
	path := filepath.Join(filepath.Dir(cfg), "tokens.toml")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("tokens.toml の権限 = %v, want 0600", st.Mode().Perm())
	}
	data, _ := os.ReadFile(path) //nolint:gosec // テストが一時フォルダに作ったパス
	sum := sha256.Sum256([]byte(plain))
	if strings.Contains(string(data), plain) || !strings.Contains(string(data), hex.EncodeToString(sum[:])) {
		t.Errorf("tokens.toml は SHA-256 だけを持つはずです:\n%s", data)
	}
	// 発行したトークンは認証を通り、ops スコープ・名前が付く。
	a, err := auth.Open(path, clock.Real{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Authenticate("Bearer " + plain)
	if err != nil || c.Name != "agent" || c.Scope != auth.ScopeOps {
		t.Errorf("Authenticate = %+v, %v", c, err)
	}
}

func TestTokenRevokeMakesAuthFail(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	_, stdout, _ := runCLI(t, "token", "add", "--scope=report", "viewer", "--config", cfg)
	plain := strings.TrimSpace(stdout)
	path := filepath.Join(filepath.Dir(cfg), "tokens.toml")
	a, err := auth.Open(path, clock.Real{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate("Bearer " + plain); err != nil {
		t.Fatalf("発行直後に通りません: %v", err)
	}
	if code, _, stderr := runCLI(t, "token", "revoke", "viewer", "--config", cfg); code != 0 {
		t.Fatalf("revoke: exit %d %s", code, stderr)
	}
	// 開いたままの Authenticator で、再起動なしに効く（NFR-SEC-6）。
	_, err = a.Authenticate("Bearer " + plain)
	if e := apperr.As(err); e.Code != apperr.Unauthorized {
		t.Errorf("失効後の Authenticate = %v, want unauthorized", err)
	}
	if code, _, _ := runCLI(t, "token", "revoke", "viewer", "--config", cfg); code == 0 {
		t.Error("失効済みの名前をもう一度失効できてしまいます")
	}
	if code, _, _ := runCLI(t, "token", "revoke", "nobody", "--config", cfg); code == 0 {
		t.Error("存在しない名前を失効できてしまいます")
	}
}

func TestTokenUsageErrors(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	for _, args := range [][]string{
		{"token"},
		{"token", "add", "x", "--config", cfg},
		{"token", "add", "x", "--scope", "admin", "--config", cfg},
		{"token", "add", "--scope", "ops", "--config", cfg},
		{"token", "add", "a", "b", "--scope", "ops", "--config", cfg},
		{"token", "revoke", "--config", cfg},
		{"token", "frobnicate"},
		{"nonsense"},
		{},
	} {
		code, stdout, _ := runCLI(t, args...)
		if code != 2 || stdout != "" {
			t.Errorf("%v: exit %d stdout=%q, want exit 2 と標準出力なし", args, code, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg), "tokens.toml")); err == nil {
		t.Error("引数の誤りなのに tokens.toml ができています")
	}
}

func TestVersion(t *testing.T) {
	if code, stdout, _ := runCLI(t, "version"); code != 0 || strings.TrimSpace(stdout) != version {
		t.Errorf("version = %d %q", code, stdout)
	}
}

// writeConfig は sessions を持つ最小の config.toml を cfg に書く。
func writeConfig(t *testing.T, cfg string, sessions string) {
	t.Helper()
	body := "listen_addr = \"127.0.0.1:48120\"\nsessions = " + sessions + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTokenAddSessions(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, cfg, `["0", "1"]`)
	tokens := filepath.Join(filepath.Dir(cfg), "tokens.toml")

	code, stdout, stderr := runCLI(t, "token", "add", "kana", "--scope", "ops", "--sessions", "0", "--config", cfg)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	kana := strings.TrimSpace(stdout)
	code, stdout, stderr = runCLI(t, "token", "add", "boss", "--scope=ops", "--sessions=0,1", "--config", cfg)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	boss := strings.TrimSpace(stdout)
	if code, stdout, _ = runCLI(t, "token", "add", "all", "--scope", "report", "--config", cfg); code != 0 {
		t.Fatalf("--sessions なし: exit %d", code)
	}
	all := strings.TrimSpace(stdout)

	a, err := auth.Open(tokens, clock.Real{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for plain, want := range map[string][]string{kana: {"0"}, boss: {"0", "1"}, all: nil} {
		c, err := a.Authenticate("Bearer " + plain)
		if err != nil || !reflect.DeepEqual(c.AllowedSessions, want) {
			t.Errorf("Authenticate = %+v, %v, want %v", c, err, want)
		}
	}
	data, _ := os.ReadFile(tokens) //nolint:gosec // テストが一時フォルダに作ったパス
	if strings.Count(string(data), "allowed_sessions") != 2 {
		t.Errorf("--sessions を省いたトークンにはキーを書かない:\n%s", data)
	}
	if strings.Contains(string(data), kana) || strings.Contains(string(data), boss) {
		t.Error("tokens.toml に平文が書かれています")
	}
}

func TestTokenAddSessionsRejected(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	tokens := filepath.Join(filepath.Dir(cfg), "tokens.toml")
	for name, c := range map[string]struct {
		config   string // "" なら config.toml を作らない
		sessions string
		wantExit int
	}{
		"config に無いセッション": {`["0", "1"]`, "0,2", 1},
		"config.toml が無い": {"", "0", 1},
		"空の要素":            {`["0", "1"]`, "0,,1", 2},
		"空文字":             {`["0", "1"]`, "", 2},
		"重複":              {`["0", "1"]`, "0,0", 1},
		"コロンを含む":          {`["0", "1"]`, "1:T1", 1},
	} {
		_ = os.Remove(cfg)
		if c.config != "" {
			writeConfig(t, cfg, c.config)
		}
		code, stdout, stderr := runCLI(t, "token", "add", "x", "--scope", "ops", "--sessions", c.sessions, "--config", cfg)
		if code != c.wantExit || stdout != "" {
			t.Errorf("%s: exit %d stdout=%q stderr=%q, want exit %d と標準出力なし", name, code, stdout, stderr, c.wantExit)
		}
		if _, err := os.Stat(tokens); err == nil {
			t.Errorf("%s: 拒否したのに tokens.toml ができています", name)
			_ = os.Remove(tokens)
		}
	}
}
