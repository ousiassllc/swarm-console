package server_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	Path     string `json:"path"`
	Size     *int64 `json:"size"`
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

// file_get の監査ログは 誰が・どのパス（実パス）・サイズ・結果コード。ファイルの中身（base64・秘密の本文）は残さない。
func TestAuditLogRecordsFileGetPathAndSizeButNeverContent(t *testing.T) {
	e := newEnv(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("AUDIT-PNG-BODY-MARKER")...)
	secret := []byte("SECRET-FILE-BODY-MARKER-4242")
	dir := e.f.Cfg.ReportDir
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	okPath := write("shot.png", png)
	envPath := write(".env", secret)
	bigPath := write("big.png", append(append([]byte{}, png...), make([]byte, 100)...))
	e.f.Cfg.FileGetMaxBytes = int64(len(png)) + 50

	cs := e.connect(t, "/ops", "boss")
	if text, isErr := callText(t, cs, "file_get", map[string]any{"path": okPath}); isErr || !strings.Contains(text, `"size":`) {
		t.Fatalf("file_get = %v %s", isErr, text)
	}
	if text, isErr := callText(t, cs, "file_get", map[string]any{"path": envPath}); !isErr || !strings.Contains(text, `"code":"path_forbidden"`) {
		t.Fatalf(".env = %v %s", isErr, text)
	}
	if text, isErr := callText(t, cs, "file_get", map[string]any{"path": bigPath}); !isErr || !strings.Contains(text, `"code":"too_large"`) {
		t.Fatalf("big = %v %s", isErr, text)
	}

	lines := auditLines(t, e)
	byPath := func(p string) *auditLine {
		return findAudit(lines, func(l auditLine) bool { return l.Tool == "file_get" && l.Path == p })
	}
	if l := byPath(okPath); l == nil || l.Client != "boss" || l.Endpoint != "/ops" || l.Code != "ok" || l.Size == nil || *l.Size != int64(len(png)) {
		t.Errorf("成功の記録 = %+v", l)
	}
	if l := byPath(envPath); l == nil || l.Client != "boss" || l.Code != "path_forbidden" || l.Size != nil {
		t.Errorf("拒否の記録 = %+v", l)
	}
	if l := byPath(bigPath); l == nil || l.Code != "too_large" || l.Size == nil || *l.Size != int64(len(png))+100 {
		t.Errorf("too_large の記録 = %+v", l)
	}
	logs := e.logs.String()
	for _, leak := range []string{
		"AUDIT-PNG-BODY-MARKER", "SECRET-FILE-BODY-MARKER", "data_base64",
		base64.StdEncoding.EncodeToString(png), base64.StdEncoding.EncodeToString(secret), e.tokens["boss"],
	} {
		if strings.Contains(logs, leak) {
			t.Errorf("監査ログに中身が残っています: %q", leak)
		}
	}
}

// file_get は /ops だけ。/report の tools/list に出ず、tools/call は unknown_tool（何も読まない）。report スコープのトークンは /ops に入れない。
func TestFileGetIsOpsOnly(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.f.Cfg.ReportDir, "shot.png")
	if err := os.WriteFile(path, append([]byte("\x89PNG\r\n\x1a\n"), 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"viewer", "boss"} { // /report は、どちらのトークンでも出ない
		cs := e.connect(t, "/report", token)
		if slices.Contains(toolNames(t, cs), "file_get") {
			t.Errorf("%s: /report の tools/list に file_get が出ています", token)
		}
		text, isErr := callText(t, cs, "file_get", map[string]any{"path": path})
		if !isErr || text != `{"error":{"code":"unknown_tool","message":"この窓口にないツールです"}}` {
			t.Errorf("%s: /report の file_get = %v %s", token, isErr, text)
		}
	}
	if !slices.Contains(toolNames(t, e.connect(t, "/ops", "boss")), "file_get") {
		t.Error("/ops の tools/list に file_get がありません")
	}
	resp, body := e.post(t, http.MethodPost, "/ops", e.tokens["viewer"],
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"file_get","arguments":{"path":"`+path+`"}}}`)
	if resp.StatusCode != 403 || body != body403 {
		t.Errorf("report トークンで /ops の file_get: %d %s", resp.StatusCode, body)
	}
	if l := findAudit(auditLines(t, e), func(l auditLine) bool { return l.Tool == "unknown" && l.Endpoint == "/report" }); l == nil || l.Path != "" {
		t.Errorf("unknown_tool の記録 = %+v", l)
	}
}
