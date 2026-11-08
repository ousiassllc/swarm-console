package pair

import (
	"bytes"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

// --scope を省略したときの出力は、--scope が無かった頃（report 固定）と同じ（--scope report を明示しても同じ）。
func TestPairScopeDefaultIsReportAndOutputUnchanged(t *testing.T) {
	for name, args := range map[string][]string{"省略": nil, "report を明示": {"--scope", "report"}} {
		e := newEnv(t, "100.64.0.5:48120", `["0", "1"]`)
		stdout, stderr, err := e.pair(args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_, plain := parseOutput(t, stdout, stderr)
		want := "swarm-viewer://pair?v=1&url=http%3A%2F%2F100.64.0.5%3A48120%2Freport&token=<TOKEN>&sessions=0%2C1\n"
		if got := strings.Replace(stdout, plain, "<TOKEN>", 1); got != want {
			t.Errorf("%s: 出力 = %q, want %q", name, got, want)
		}
		c, err := e.open(t).Authenticate("Bearer " + plain)
		if err != nil || c.Scope != auth.ScopeReport || !strings.HasPrefix(c.Name, "viewer-") || strings.HasPrefix(c.Name, "viewer-ops-") {
			t.Errorf("%s: %+v, %v（report スコープで viewer-<ホスト名>- 始まりのはず）", name, c, err)
		}
		if strings.Contains(stderr, "警告") || !strings.HasPrefix(stderr, "report スコープの token ") {
			t.Errorf("%s: report の案内に ops の警告が混ざっています:\n%s", name, stderr)
		}
	}
}

// opsNameRe は ops の自動名 viewer-ops-<ホスト名>-<yyyymmdd>。
var opsNameRe = regexp.MustCompile(`^viewer-ops-[a-z0-9_-]+-[0-9]{8}$`)

func TestPairScopeOpsIssuesOpsTokenWithOpsURLAndWarning(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0", "1"]`)
	stdout, stderr, err := e.pair("--scope", "ops")
	if err != nil {
		t.Fatal(err)
	}
	q, plain := parseOutputPath(t, stdout, stderr, "/ops")
	want := "swarm-viewer://pair?v=1&url=http%3A%2F%2F100.64.0.5%3A48120%2Fops&token=<TOKEN>&sessions=0%2C1\n"
	if got := strings.Replace(stdout, plain, "<TOKEN>", 1); got != want {
		t.Errorf("出力 = %q, want %q", got, want)
	}
	if q.Get("v") != "1" {
		t.Errorf("v = %q（形式は変えない）", q.Get("v"))
	}
	c, err := e.open(t).Authenticate("Bearer " + plain)
	if err != nil || c.Scope != auth.ScopeOps || !opsNameRe.MatchString(c.Name) {
		t.Fatalf("Authenticate = %+v, %v（ops スコープで viewer-ops-<ホスト名>-<yyyymmdd> のはず）", c, err)
	}
	if c.AllowedSessions != nil {
		t.Errorf("--sessions を省略したら allowed_sessions は書かないはず: %v", c.AllowedSessions)
	}
	for _, w := range []string{
		"ops スコープの token", "【警告】", "送信・中断・起動・終了", "token revoke " + c.Name, "--sessions", "今回しか表示されません",
	} {
		if !strings.Contains(stderr, w) {
			t.Errorf("標準エラーに %q がありません:\n%s", w, stderr)
		}
	}
}

// ops でも --sessions は効き、絞ったときは「絞ることをお勧めします」を出さない（警告本体は出る）。
func TestPairScopeOpsWithSessions(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0", "1", "2"]`)
	stdout, stderr, err := e.pair("--scope", "ops", "--sessions", "1")
	if err != nil {
		t.Fatal(err)
	}
	q, plain := parseOutputPath(t, stdout, stderr, "/ops")
	if q.Get("sessions") != "1" {
		t.Errorf("sessions = %q", q.Get("sessions"))
	}
	c, err := e.open(t).Authenticate("Bearer " + plain)
	if err != nil || c.Scope != auth.ScopeOps || !reflect.DeepEqual(c.AllowedSessions, []string{"1"}) {
		t.Fatalf("Authenticate = %+v, %v", c, err)
	}
	if !strings.Contains(stderr, "【警告】") || strings.Contains(stderr, "お勧めします") {
		t.Errorf("警告は出し、絞り込みの勧めは出さないはず:\n%s", stderr)
	}
}

// --name・--scope ops の組み合わせでも接続文字列の形式（v=1・パラメータの順）は同じ。
func TestPairScopeOpsWithName(t *testing.T) {
	e := newEnv(t, "[fd7a:115c:a1e0::5]:48120", `["0"]`)
	stdout, stderr, err := e.pair("--scope", "ops", "--name", "devbox")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := parseOutputPath(t, stdout, stderr, "/ops")
	if q.Get("name") != "devbox" || q.Get("url") != "http://[fd7a:115c:a1e0::5]:48120/ops" {
		t.Errorf("name=%q url=%q", q.Get("name"), q.Get("url"))
	}
}

func TestPairScopeInvalidIsUsageErrorWithoutIssuing(t *testing.T) {
	for name, scope := range map[string]string{
		"未知の値":     "admin",
		"空文字":      "",
		"大文字":      "OPS",
		"カンマ区切り":   "report,ops",
		"前後に空白":    " ops",
		"ops の接頭辞": "op",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, "100.64.0.5:48120", `["0"]`)
			stdout, _, err := e.pair("--scope", scope)
			if !errors.Is(err, ErrUsage) || stdout != "" {
				t.Errorf("err=%v stdout=%q, want 使い方の誤り", err, stdout)
			}
			e.requireNoTokensFile(t)
		})
	}
}

// ops の自動名の衝突は report と同じく -2・-3… で避ける。report と ops は名前の先頭が違うので重ならない。
func TestPairOpsAutoTokenNameAvoidsCollisions(t *testing.T) {
	e := newEnv(t, "100.64.0.5:48120", `["0"]`)
	deps := fixedDeps("devbox")
	issueOne := func(scope auth.Scope) string {
		t.Helper()
		var out, errb bytes.Buffer
		if err := issue(Options{ConfigPath: e.cfg, Scope: scope}, deps, &out, &errb); err != nil {
			t.Fatal(err)
		}
		_, plain := parseOutputPath(t, out.String(), errb.String(), "/"+string(scope))
		c, err := e.open(t).Authenticate("Bearer " + plain)
		if err != nil {
			t.Fatal(err)
		}
		return c.Name
	}
	for i, want := range []string{"viewer-ops-devbox-20261104", "viewer-ops-devbox-20261104-2", "viewer-ops-devbox-20261104-3"} {
		if got := issueOne(auth.ScopeOps); got != want {
			t.Fatalf("%d 回目の ops の名前 = %q, want %q", i+1, got, want)
		}
	}
	if got := issueOne(auth.ScopeReport); got != "viewer-devbox-20261104" {
		t.Errorf("report の名前 = %q（ops と重ならないはず）", got)
	}
}

// ops の接頭辞は report より長いので、ホスト名を短く切って、-100 まで付けても token 名の上限（32 文字）に収める。
func TestTokenBaseOpsFitsNameLimit(t *testing.T) {
	deps := fixedDeps("a-very-long-host-name.example.com")
	got := tokenBase(deps, auth.ScopeOps)
	if got != "viewer-ops-a-very-l-20261104" {
		t.Errorf("tokenBase(ops) = %q", got)
	}
	if err := auth.ValidateName(got + "-100"); err != nil {
		t.Errorf("%q-100 は token 名として不正: %v", got, err)
	}
}
