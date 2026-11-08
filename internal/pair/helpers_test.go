package pair

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

var tokenRe = regexp.MustCompile(`^sc_[A-Za-z0-9_-]{43}$`)

// env は listen_addr と sessions を持つ config.toml を一時フォルダに作ったもの。tokens はその隣の tokens.toml。
type env struct {
	cfg    string
	tokens string
}

// newEnv は listen が空なら listen_addr を書かない。トークンは実コード（auth.Add）で発行するので固定値のファイルは持たない。
func newEnv(t *testing.T, listen, sessions string) env {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := "sessions = " + sessions + "\n"
	if listen != "" {
		body = "listen_addr = \"" + listen + "\"\n" + body
	}
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return env{cfg: cfg, tokens: filepath.Join(filepath.Dir(cfg), "tokens.toml")}
}

// pair は pair を実行する。
func (e env) pair(args ...string) (stdout, stderr string, err error) {
	var out, errb bytes.Buffer
	err = Run(append([]string{"--config", e.cfg}, args...), &out, &errb)
	return out.String(), errb.String(), err
}

func (e env) requireNoTokensFile(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(e.tokens); err == nil {
		t.Fatal("token を発行していないはずなのに tokens.toml ができています")
	}
}

func (e env) open(t *testing.T) *auth.Authenticator {
	t.Helper()
	a, err := auth.Open(e.tokens, clock.Real{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// mustPair は pair を実行して成功を確かめ、出力を検証してパラメータと平文 token を返す。
func (e env) mustPair(t *testing.T, args ...string) (q url.Values, plain, stdout string) {
	t.Helper()
	stdout, stderr, err := e.pair(args...)
	if err != nil {
		t.Fatalf("pair %v: %v", args, err)
	}
	q, plain = parseOutput(t, stdout, stderr)
	return q, plain, stdout
}

// parseOutput は接続文字列を net/url で読み、swarm-viewer が受け付ける条件を確かめて、パラメータと平文 token を返す。
func parseOutput(t *testing.T, stdout, stderr string) (q url.Values, plain string) {
	t.Helper()
	return parseOutputPath(t, stdout, stderr, "/report")
}

// parseOutputPath は parseOutput で、url の path が wantPath（/report か /ops）であることも確かめる。
func parseOutputPath(t *testing.T, stdout, stderr, wantPath string) (q url.Values, plain string) {
	t.Helper()
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("標準出力は 1 行だけのはず: %q", stdout)
	}
	line := strings.TrimSuffix(stdout, "\n")
	if len(line) > 4096 {
		t.Fatalf("%d バイトで 4096 を超えます", len(line))
	}
	u, err := url.Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "swarm-viewer" || u.Host != "pair" || u.Path != "" || u.Fragment != "" || u.User != nil {
		t.Fatalf("scheme/host が違います: %q", line)
	}
	for _, kv := range strings.Split(u.RawQuery, "&") {
		key, _, _ := strings.Cut(kv, "=")
		if strings.Count("&"+u.RawQuery, "&"+key+"=") != 1 {
			t.Fatalf("パラメータ %q が重複しています: %q", key, line)
		}
	}
	q = u.Query()
	if q.Get("v") != "1" {
		t.Fatalf("v = %q", q.Get("v"))
	}
	plain = q.Get("token")
	if !tokenRe.MatchString(plain) {
		t.Fatalf("token の形式が違います")
	}
	if strings.Contains(stderr, plain) {
		t.Error("標準エラーに token が出ています")
	}
	vu, err := url.Parse(q.Get("url"))
	if err != nil || vu.Scheme != "http" || vu.Path != wantPath || vu.RawQuery != "" || vu.Fragment != "" || vu.User != nil || vu.Port() != "48120" {
		t.Fatalf("url が http://<IP>:48120%s の形ではありません: %q", wantPath, q.Get("url"))
	}
	return q, plain
}

// fixedDeps は token 名が決まる要素（ホスト名・時刻）を固定する。
func fixedDeps(host string) Deps {
	return Deps{
		Hostname: func() (string, error) { return host, nil },
		Now:      func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) },
	}
}
