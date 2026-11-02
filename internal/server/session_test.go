package server_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// sessionEnv は 2 セッション（0・1）の構成。トークン kana=0 だけ・natsumi=1 だけ・boss=両方（boss は newEnv が作る許可なし）。
func sessionEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.f.UseTwoSessions()
	for name, sessions := range map[string][]string{"kana": {"0"}, "natsumi": {"1"}} {
		plain, err := auth.Add(e.tokPath, name, auth.ScopeOps, toolstest.At, sessions...)
		if err != nil {
			t.Fatal(err)
		}
		e.tokens[name] = plain
	}
	return e
}

func resourceURIs(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	list, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var uris []string
	for _, r := range list.Resources {
		uris = append(uris, r.URI)
	}
	return uris
}

// resources/list は許可されたセッションのペインだけ。許可が 1 つでないと swarm://report は出ない。
func TestResourcesListFollowsACL(t *testing.T) {
	e := sessionEnv(t)
	kana := resourceURIs(t, e.connect(t, "/ops", "kana"))
	if len(kana) != 1+2*5 || !slices.Contains(kana, "swarm://report") || !slices.Contains(kana, "swarm://pane/T1") || !slices.Contains(kana, "swarm://notes/B3") {
		t.Errorf("kana = %v", kana)
	}
	natsumi := resourceURIs(t, e.connect(t, "/report", "natsumi"))
	if len(natsumi) != 1+2*6 || !slices.Contains(natsumi, "swarm://pane/T3") {
		t.Errorf("natsumi = %v", natsumi)
	}
	for _, u := range kana {
		if strings.Contains(u, "1:") || strings.Contains(u, "%2511") {
			t.Errorf("kana にセッション 1 の resource が出ています: %s", u)
		}
	}
	boss := resourceURIs(t, e.connect(t, "/ops", "boss"))
	if slices.Contains(boss, "swarm://report") || !slices.Contains(boss, "swarm://pane/1%3AT1") || !slices.Contains(boss, "swarm://pane/T1") {
		t.Errorf("許可が複数の boss = %v（swarm://report なし・全体の名前）", boss)
	}
}

func TestResourcesReadErrorsFollowACL(t *testing.T) {
	e := sessionEnv(t)
	kana := e.connect(t, "/ops", "kana")
	for _, uri := range []string{"swarm://pane/%2511", "swarm://notes/%2511", "swarm://pane/1%3AT1", "swarm://notes/1%3AT1"} {
		_, err := readRes(t, kana, uri)
		var je *jsonrpc.Error
		if !errors.As(err, &je) || je.Code != -32002 || !strings.Contains(string(je.Data), `"code":"session_forbidden"`) ||
			!strings.Contains(string(je.Data), `"message":"このセッションは許可されていません"`) || !strings.Contains(string(je.Data), `"session":"1"`) {
			t.Errorf("%s: err = %v", uri, err)
		}
	}
	// 許可内のセッションの中の位置。
	if c, err := readRes(t, e.connect(t, "/ops", "natsumi"), "swarm://notes/T3"); err != nil || !strings.Contains(c.Text, `"id":"%13"`) {
		t.Errorf("natsumi の T3: %v %+v", err, c)
	}
	if c, err := readRes(t, e.connect(t, "/ops", "boss"), "swarm://pane/1%3AT1"); err != nil || !strings.Contains(c.Text, "%11") {
		t.Errorf("boss の 1:T1: %v %+v", err, c)
	}
	// 許可が複数: swarm://report は session_required（-32602）。
	_, err := readRes(t, e.connect(t, "/ops", "boss"), "swarm://report")
	var je *jsonrpc.Error
	if !errors.As(err, &je) || je.Code != -32602 || !strings.Contains(string(je.Data), `"code":"session_required"`) {
		t.Errorf("boss の swarm://report: %v", err)
	}
}

// ツールの失敗本文（isError）は API 仕様 §3.1 の形・固定の日本語文。
func TestSessionErrorBodies(t *testing.T) {
	e := sessionEnv(t)
	text, isErr := callText(t, e.connect(t, "/ops", "kana"), "panes_status", map[string]any{"session": "1"})
	if !isErr || text != `{"error":{"code":"session_forbidden","message":"このセッションは許可されていません","details":{"session":"1"}}}` {
		t.Errorf("session_forbidden = %v %s", isErr, text)
	}
	text, isErr = callText(t, e.connect(t, "/ops", "boss"), "panes_status", nil)
	if !isErr || text != `{"error":{"code":"session_required","message":"session を指定してください","details":{"allowed_count":2}}}` {
		t.Errorf("session_required = %v %s", isErr, text)
	}
}

// dispatch の prompt は任意の session を先頭に受け取り、そのままツールの引数に渡す。省略もできる（単一セッションの互換）。
func TestDispatchPromptSessionArg(t *testing.T) {
	e := sessionEnv(t)
	cs := e.connect(t, "/ops", "kana")
	got := promptText(t, cs, "dispatch", map[string]string{"session": "0", "pane": "T1", "agent": "omp", "task": "やる", "profile": "fix"})
	if !strings.HasSuffix(got, `{"session":"0","pane":"T1","agent":"omp","task":"やる","profile":"fix"}`) {
		t.Errorf("dispatch prompt = %s", got)
	}
	got = promptText(t, cs, "dispatch", map[string]string{"pane": "T1", "task": "やる", "profile": "fix"})
	if strings.Contains(got, "session") && strings.Contains(strings.SplitN(got, "引数:", 2)[1], `"session"`) {
		t.Errorf("省略した session が出ています: %s", got)
	}
	prompts, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prompts.Prompts {
		if p.Name != "dispatch" {
			continue
		}
		if p.Arguments[0].Name != "session" || p.Arguments[0].Required {
			t.Errorf("session は先頭の任意引数: %+v", p.Arguments[0])
		}
	}
}

// 監査ログには session 引数（送られたまま・短く切る）が pane と並んで残る。トークンや本文は残らない。
func TestAuditLogRecordsSession(t *testing.T) {
	e := sessionEnv(t)
	cs := e.connect(t, "/ops", "kana")
	callText(t, cs, "panes_status", map[string]any{"session": "1"})
	callText(t, cs, "pane_tail", map[string]any{"session": "0", "pane": "T1"})
	callText(t, cs, "panes_status", map[string]any{"session": strings.Repeat("s", 100)})
	callText(t, cs, "panes_status", nil)
	lines := auditLines(t, e)
	if l := findAudit(lines, func(l auditLine) bool { return l.Tool == "panes_status" && l.Session == "1" }); l == nil || l.Client != "kana" || l.Code != "session_forbidden" {
		t.Errorf("許可外の記録 = %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return l.Tool == "pane_tail" }); l == nil || l.Session != "0" || l.Pane != "T1" || l.Code != "ok" {
		t.Errorf("pane_tail の記録 = %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return strings.HasPrefix(l.Session, "ssss") }); l == nil || len(l.Session) != 40 {
		t.Errorf("長い session は 40 字で切る: %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return l.Tool == "panes_status" && l.Session == "" }); l == nil || l.Code != "ok" {
		t.Errorf("session 省略の記録 = %+v", l)
	}
	for _, secret := range []string{e.tokens["kana"], e.tokens["natsumi"], e.tokens["boss"]} {
		if strings.Contains(e.logs.String(), secret) {
			t.Error("ログにトークンが出ています")
		}
	}
}
