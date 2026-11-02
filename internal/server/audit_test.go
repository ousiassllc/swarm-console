package server_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

type auditLine struct {
	Msg      string `json:"msg"`
	Client   string `json:"client"`
	Endpoint string `json:"endpoint"`
	Method   string `json:"method"`
	Tool     string `json:"tool"`
	Pane     string `json:"pane"`
	Session  string `json:"session"`
	Code     string `json:"code"`
	Status   int    `json:"status"`
}

func auditLines(t *testing.T, e *env) []auditLine {
	t.Helper()
	var out []auditLine
	sc := bufio.NewScanner(strings.NewReader(e.logs.String()))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var l auditLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("ログが JSON Lines ではありません: %q", sc.Text())
		}
		if l.Msg == "audit" {
			out = append(out, l)
		}
	}
	return out
}

func findAudit(lines []auditLine, f func(auditLine) bool) *auditLine {
	for i := range lines {
		if f(lines[i]) {
			return &lines[i]
		}
	}
	return nil
}

// 監査ログは クライアント名・窓口・ツール・ペイン・結果コードだけ。送った本文・トークン・メモの内容・ヘッダーを残さない。
func TestAuditLogRecordsWhoWhatAndNeverBodies(t *testing.T) {
	e := newEnv(t)
	secretBody := "PASTE-BODY-MARKER-12345"
	secretNote := "NOTE-CONTENT-MARKER-67890"
	e.f.Ops.Send = pane.SendResult{OK: true, State: agent.StateWorking, Accepted: true, Chars: 23}
	cs := e.connect(t, "/ops", "boss")
	callText(t, cs, "send_text", map[string]any{"pane": "T1", "text": secretBody})
	callText(t, cs, "note_set", map[string]any{"pane": "T1", "set": map[string]any{"memo": secretNote}})
	callText(t, cs, "dispatch", map[string]any{
		"pane": "T1", "agent": "omp", "task": secretBody, "profile": "fix", "note": map[string]any{"memo": secretNote},
	})
	callText(t, cs, "pane_tail", map[string]any{"pane": "Z9"})
	callText(t, e.connect(t, "/report", "viewer"), "panes_status", nil)
	_, _ = e.post(t, http.MethodPost, "/ops", "", "{}")
	_, _ = e.post(t, http.MethodPost, "/ops", e.tokens["viewer"], "{}")

	logs := e.logs.String()
	lines := auditLines(t, e)
	if l := findAudit(lines, func(l auditLine) bool { return l.Tool == "send_text" }); l == nil ||
		l.Client != "boss" || l.Endpoint != "/ops" || l.Pane != "T1" || l.Code != "ok" || l.Method != "tools/call" {
		t.Errorf("send_text の記録 = %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return l.Tool == "pane_tail" }); l == nil || l.Code != "pane_not_found" || l.Pane != "Z9" {
		t.Errorf("失敗の記録 = %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return l.Tool == "panes_status" }); l == nil || l.Client != "viewer" || l.Endpoint != "/report" {
		t.Errorf("panes_status の記録 = %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return l.Status == 401 }); l == nil || l.Code != "unauthorized" || l.Client != "" {
		t.Errorf("401 の記録 = %+v", l)
	}
	if l := findAudit(lines, func(l auditLine) bool { return l.Status == 403 }); l == nil || l.Code != "forbidden_scope" || l.Client != "viewer" {
		t.Errorf("403 の記録 = %+v", l)
	}
	for _, secret := range []string{
		secretBody, secretNote, e.tokens["boss"], e.tokens["viewer"], "Bearer", "Authorization", "authorization",
	} {
		if strings.Contains(logs, secret) {
			t.Errorf("ログに %q が出ています:\n%s", secret, logs)
		}
	}
}

// 認証前に拒否したリクエストの URL パスなど、クライアントが選べる文字列をそのままログに出さない。
func TestAuditLogDoesNotEchoPath(t *testing.T) {
	e := newEnv(t)
	_, _ = e.post(t, http.MethodPost, "/PATH-MARKER-xyz", "", "{}")
	if strings.Contains(e.logs.String(), "PATH-MARKER") {
		t.Errorf("ログにパスが出ています:\n%s", e.logs.String())
	}
}
