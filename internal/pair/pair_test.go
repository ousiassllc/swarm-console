package pair

import (
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

func TestPairIssuesReportTokenAndPrintsOneLine(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0", "1"]`)
	stdout, stderr, err := e.pair()
	if err != nil {
		t.Fatal(err)
	}
	q, plain := parseOutput(t, stdout, stderr)
	want := "swarm-viewer://pair?v=1&url=http%3A%2F%2F100.64.0.5%3A48120%2Freport&token=<TOKEN>&sessions=0%2C1\n"
	if got := strings.Replace(stdout, plain, "<TOKEN>", 1); got != want {
		t.Errorf("出力 = %q, want %q", got, want)
	}
	if q.Has("name") {
		t.Error("--name を渡していないのに name が付いています")
	}
	c, err := e.open(t).Authenticate("Bearer " + plain)
	if err != nil || c.Scope != auth.ScopeReport || !strings.HasPrefix(c.Name, "viewer-") {
		t.Fatalf("Authenticate = %+v, %v（report スコープで viewer- 始まりの名前のはず）", c, err)
	}
	if c.AllowedSessions != nil {
		t.Errorf("--sessions を省略したら allowed_sessions は書かない（全セッション）はず: %v", c.AllowedSessions)
	}
	for _, w := range []string{"今回しか表示されません", "履歴", "使い回さない", "token revoke " + c.Name} {
		if !strings.Contains(stderr, w) {
			t.Errorf("標準エラーの案内に %q がありません:\n%s", w, stderr)
		}
	}
}

func TestPairUsesIPv6TailnetAddress(t *testing.T) {
	e := newEnv(t, "[fd7a:115c:a1e0::5]:48120", `["0"]`)
	q, _, _ := e.mustPair(t)
	if got := q.Get("url"); got != "http://[fd7a:115c:a1e0::5]:48120/report" {
		t.Errorf("url = %q", got)
	}
}

func TestPairSessionsGivenAndOmitted(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0", "1", "2"]`)

	q, plain, stdout := e.mustPair(t, "--sessions", "1, 2")
	if q.Get("sessions") != "1,2" || !strings.Contains(stdout, "&sessions=1%2C2") {
		t.Errorf("sessions = %q（%q）", q.Get("sessions"), stdout)
	}
	c, err := e.open(t).Authenticate("Bearer " + plain)
	if err != nil || !reflect.DeepEqual(c.AllowedSessions, []string{"1", "2"}) {
		t.Errorf("--sessions を渡したら許可セッションはその指定だけのはず: %+v, %v", c, err)
	}

	q, plain, _ = e.mustPair(t)
	if q.Get("sessions") != "0,1,2" {
		t.Errorf("省略したら config.toml の sessions 全部のはず: %q", q.Get("sessions"))
	}
	if c, err = e.open(t).Authenticate("Bearer " + plain); err != nil || c.AllowedSessions != nil {
		t.Errorf("省略時は allowed_sessions なし: %+v, %v", c, err)
	}
}

func TestPairNameParameter(t *testing.T) {
	for _, name := range []string{
		"devbox",
		"my box",
		"a&b=c?d#e%f",
		"開発機 🖥",
		"a+b",
		strings.Repeat("あ", 100),
		"x",
		"記号 /:;@,\"'<>",
	} {
		e := newEnv(t, "100.64.0.5:48120", `["0"]`)
		q, plain, stdout := e.mustPair(t, "--name", name)
		if q.Get("name") != name {
			t.Errorf("name = %q, want %q（%q）", q.Get("name"), name, stdout)
		}
		if strings.ContainsAny(stdout, " +") {
			t.Errorf("空白は %%20、+ は %%2B にエンコードされるはず: %q", stdout)
		}
		if _, err := e.open(t).Authenticate("Bearer " + plain); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestPairIssuedTokenIsListedAndRevocable(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0", "1"]`)
	_, plain, _ := e.mustPair(t, "--sessions", "1")
	a := e.open(t)
	c, err := a.Authenticate("Bearer " + plain)
	if err != nil {
		t.Fatal(err)
	}
	list := func() string {
		t.Helper()
		var out, errb strings.Builder
		if err := app.TokenList(e.cfg, &out, &errb); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	out := list()
	if f := strings.Fields(out); strings.Count(out, "\n") != 1 || len(f) != 5 ||
		f[0] != c.Name || f[1] != "report" || f[2] != "1" || f[4] != "active" {
		t.Errorf("token list = %q", out)
	}
	if strings.Contains(out, plain) || strings.Contains(out, "sha256") {
		t.Error("token list に平文かハッシュが出ています")
	}

	if err := app.TokenRevoke(e.cfg, c.Name, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate("Bearer " + plain); err == nil {
		t.Error("失効後も認証が通ります")
	}
	if out = list(); !strings.Contains(out, c.Name) || !strings.Contains(out, "revoked ") || strings.Contains(out, "active") {
		t.Errorf("失効後の token list = %q", out)
	}
}

func TestHelpMentionsSecurityNotes(t *testing.T) {
	_, stderr, err := (env{cfg: "unused"}).pair("--help")
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("--help は flag.ErrHelp のはず: %v", err)
	}
	for _, w := range []string{"標準出力", "履歴", "チャット", "使い回さない", "token revoke", "pair をやり直", "report スコープ", "ops は発行しません", "--sessions", "--name"} {
		if !strings.Contains(stderr, w) {
			t.Errorf("pair --help に %q がありません:\n%s", w, stderr)
		}
	}
}
