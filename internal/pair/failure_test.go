package pair

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

func TestPairRejectsInvalidSessionsWithoutIssuing(t *testing.T) {
	for name, c := range map[string]struct {
		args      []string
		wantUsage bool
	}{
		"config に無いセッション": {[]string{"--sessions", "0,9"}, false},
		"空の要素":            {[]string{"--sessions", "0,,1"}, true},
		"空文字":             {[]string{"--sessions", ""}, true},
		"重複":              {[]string{"--sessions", "0,0"}, false},
		"位置引数":            {[]string{"extra"}, true},
		"未知のフラグ":          {[]string{"--nope"}, true},
	} {
		e := newEnv(t, "100.64.0.5:48120", `["0", "1"]`)
		stdout, _, err := e.pair(c.args...)
		if err == nil || stdout != "" || errors.Is(err, ErrUsage) != c.wantUsage {
			t.Errorf("%s: err=%v stdout=%q, want 使い方の誤り=%v", name, err, stdout, c.wantUsage)
		}
		e.requireNoTokensFile(t)
	}
}

func TestPairRejectsInvalidNameWithoutIssuing(t *testing.T) {
	for name, n := range map[string]string{
		"空":           "",
		"101 文字":      strings.Repeat("a", 101),
		"101 文字(日本語)": strings.Repeat("あ", 101),
		"改行":          "a\nb",
		"タブ":          "a\tb",
		"ESC":         "a\x1bb",
		"不正な UTF-8":   "a\xffb",
	} {
		e := newEnv(t, "100.64.0.5:48120", `["0"]`)
		stdout, _, err := e.pair("--name", n)
		if !errors.Is(err, ErrUsage) || stdout != "" {
			t.Errorf("%s: err=%v stdout=%q", name, err, stdout)
		}
		e.requireNoTokensFile(t)
	}
}

func TestPairFailsWithoutTailnetIPAndDoesNotIssue(t *testing.T) {
	for name, listen := range map[string]string{
		"ループバック":         "127.0.0.1:48120",
		"IPv6 ループバック":    "[::1]:48120",
		"0.0.0.0":        "0.0.0.0:48120",
		"ホスト名":           "devbox:48120",
		"ポート違い(ループバック)":  "127.0.0.1:8080",
		"ポート違い(tailnet)": "100.64.0.5:8080",
		"ポート無し":          "100.64.0.5",
		"範囲外の IP":        "192.168.1.5:48120",
		"listen_addr なし": "",
	} {
		e := newEnv(t, listen, `["0"]`)
		stdout, _, err := e.pair()
		if err == nil || errors.Is(err, ErrUsage) || stdout != "" {
			t.Errorf("%s: err=%v stdout=%q, want 使い方以外のエラーと標準出力なし", name, err, stdout)
		}
		e.requireNoTokensFile(t)
	}
	var out, errb bytes.Buffer
	if err := Run([]string{"--config", t.TempDir() + "/config.toml"}, &out, &errb); err == nil || out.Len() != 0 {
		t.Errorf("config.toml なし: err=%v stdout=%q", err, out.String())
	}
}

func TestPairLoopbackErrorLeavesExistingTokensUntouched(t *testing.T) {
	e := newEnv(t, "127.0.0.1:48120", `["0"]`)
	if _, err := auth.Add(e.tokens, "existing", auth.ScopeReport, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.tokens)
	if err != nil {
		t.Fatal(err)
	}
	if stdout, _, err := e.pair(); err == nil || stdout != "" {
		t.Fatalf("err=%v stdout=%q", err, stdout)
	}
	after, _ := os.ReadFile(e.tokens)
	if !bytes.Equal(before, after) {
		t.Error("tokens.toml が書き換わっています")
	}
}

func TestPairRejectsOversizeConnectionStringWithoutIssuing(t *testing.T) {
	var quoted, plain []string
	for i := range 9 {
		s := strings.Repeat("s", 500) + strconv.Itoa(i)
		quoted, plain = append(quoted, strconv.Quote(s)), append(plain, s)
	}
	list := "[" + strings.Join(quoted, ", ") + "]"
	for name, args := range map[string][]string{
		"sessions 省略（config の全セッション）": nil,
		"--sessions 指定": {"--sessions", strings.Join(plain, ",")},
	} {
		e := newEnv(t, "100.64.0.5:48120", list)
		stdout, _, err := e.pair(args...)
		if err == nil || stdout != "" || !strings.Contains(err.Error(), "4096") {
			t.Errorf("%s: err=%v stdout=%q", name, err, stdout)
		}
		e.requireNoTokensFile(t)
	}
}

func TestPairConnectionStringAtLimitIsAccepted(t *testing.T) {
	const viewer = "http://100.64.0.5:48120/report"
	base := len(connectionString(viewer, placeholderToken, "", []string{""}))
	pad := strings.Repeat("x", maxBytes-base)
	if got := len(connectionString(viewer, placeholderToken, "", []string{pad})); got != maxBytes {
		t.Fatalf("len = %d, want %d", got, maxBytes)
	}
	e := newEnv(t, "100.64.0.5:48120", `["`+pad+`"]`)
	_, _, stdout := e.mustPair(t)
	if n := len(strings.TrimSuffix(stdout, "\n")); n != maxBytes {
		t.Errorf("出力 %d バイト, want %d", n, maxBytes)
	}
}

func TestPairAutoTokenNameAvoidsCollisions(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0"]`)
	deps := fixedDeps("devbox")
	issueOne := func() string {
		t.Helper()
		var out, errb bytes.Buffer
		if err := issue(Options{ConfigPath: e.cfg}, deps, &out, &errb); err != nil {
			t.Fatal(err)
		}
		_, plain := parseOutput(t, out.String(), errb.String())
		c, err := e.open(t).Authenticate("Bearer " + plain)
		if err != nil {
			t.Fatal(err)
		}
		return c.Name
	}
	if got := issueOne(); got != "viewer-devbox-20261104" {
		t.Fatalf("1 回目の名前 = %q", got)
	}
	if got := issueOne(); got != "viewer-devbox-20261104-2" {
		t.Fatalf("同じ日の 2 回目の名前 = %q", got)
	}
	// 失効済みの名前は再利用できないので、次の空き番号になる。
	if err := auth.Revoke(e.tokens, "viewer-devbox-20261104", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := issueOne(); got != "viewer-devbox-20261104-3" {
		t.Fatalf("失効済みと重なったときの名前 = %q", got)
	}
}

func TestPairFailsWhenAllAutoNamesAreTaken(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0"]`)
	base := "viewer-devbox-20261104"
	if _, err := auth.Add(e.tokens, base, auth.ScopeReport, time.Now()); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= nameTries; n++ {
		if _, err := auth.Add(e.tokens, base+"-"+strconv.Itoa(n), auth.ScopeReport, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	err := issue(Options{ConfigPath: e.cfg}, fixedDeps("devbox"), &out, &errb)
	if !errors.Is(err, auth.ErrNameInUse) || out.Len() != 0 {
		t.Fatalf("err=%v stdout=%q", err, out.String())
	}
	if infos, err := auth.List(e.tokens); err != nil || len(infos) != nameTries {
		t.Errorf("token が増えています: %d 件, %v", len(infos), err)
	}
}

func TestTokenBaseSanitizesHostname(t *testing.T) {
	for name, c := range map[string]struct {
		host string
		err  error
		want string
	}{
		"通常":         {host: "devbox", want: "viewer-devbox-20261104"},
		"大文字とドット":    {host: "DevBox.local", want: "viewer-devbox-local-20261104"},
		"空":          {host: "", want: "viewer-host-20261104"},
		"取得失敗":       {err: errors.New("boom"), want: "viewer-host-20261104"},
		"日本語だけ":      {host: "ホスト", want: "viewer-host-20261104"},
		"長い":         {host: "a-very-long-host-name.example.com", want: "viewer-a-very-long-20261104"},
		"記号で始まる":     {host: "--_x", want: "viewer-x-20261104"},
		"切り詰めで末尾が -": {host: "abcdefghijk-lmn", want: "viewer-abcdefghijk-20261104"},
	} {
		deps := fixedDeps(c.host)
		deps.Hostname = func() (string, error) { return c.host, c.err }
		got := tokenBase(deps)
		if got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
		// 末尾に -100 まで付けても token 名の規則（32 文字以内）に合う。
		if err := auth.ValidateName(got + "-100"); err != nil {
			t.Errorf("%s: %q-100 は token 名として不正: %v", name, got, err)
		}
	}
}
