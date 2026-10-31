package tooltests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

const dispatchOK = `"agent":"omp","task":"#982 を直す","profile":"fix","command":"/issue-sweep #982"`

func wantSent(f *toolstest.Fixture, pos string) string {
	return "/issue-sweep #982\n\n## やること\n#982 を直す\n\n## 条件\n- 報告は " +
		filepath.Join(f.Cfg.ReportDir, pos+".md") + " に書く\n- 小さく直す\n"
}

func TestDispatchIdle(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true}
	m := mustCall(t, f, "dispatch", `{"pane":"T1",`+dispatchOK+`,"cwd":"/ignored/for/idle","note":{"from":"agent-b","items":["#982"]}}`)
	if m["sent"] != wantSent(f, "T1") || m["ok"] != true || m["started"] != false || m["accepted"] != true || m["note_saved"] != true {
		t.Errorf("dispatch = %v", m)
	}
	if f.Ops.LastDispatch.Text != wantSent(f, "T1") || f.Ops.LastDispatch.Agent != "omp" || f.Ops.LastDispatch.Cwd != "" {
		t.Errorf("Ops への引数 = %+v", f.Ops.LastDispatch)
	}
	if n, ok := f.Notes.Get("%1"); !ok || n.From != "agent-b" {
		t.Errorf("メモが保存されていません: %+v", n)
	}
}

func TestDispatchShellStartsPane(t *testing.T) {
	f := toolstest.New(t)
	cwd := filepath.Join(f.Root, "wt")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Started: true}
	m := mustCall(t, f, "dispatch", `{"pane":"B1",`+dispatchOK+`,"cwd":"`+cwd+`","title":"funid #982"}`)
	if m["started"] != true || m["note_saved"] != false {
		t.Errorf("dispatch = %v", m)
	}
	if got := f.Ops.LastDispatch; got.Cwd != cwd || got.Title != "funid #982" {
		t.Errorf("Ops への引数 = %+v", got)
	}
	if text := mustCall(t, f, "dispatch", `{"pane":"B1",`+dispatchOK+`,"cwd":"`+cwd+`","title":"t","extra_rules":["追加の条件"]}`)["sent"].(string); !strings.HasSuffix(text, "- 追加の条件\n") {
		t.Errorf("extra_rules が末尾に入っていません: %q", text)
	}
}

// 事前検証で 1 つでも落ちたら、何も送らず、メモも保存しない。
func TestDispatchPreflightSendsAndSavesNothing(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: true}
	secret := "ghp_" + strings.Repeat("Z9y8", 8)
	cases := []struct {
		name, args string
		code       apperr.Code
		field      string
	}{
		{"秘密を含む note", `{"pane":"T1",` + dispatchOK + `,"note":{"memo":"` + secret + `"}}`, apperr.SecretDetected, "memo"},
		{"不正な note", `{"pane":"T1",` + dispatchOK + `,"note":{"nope":"x"}}`, apperr.InvalidArgs, "nope"},
		{"task の制御文字", `{"pane":"T1","agent":"omp","profile":"fix","task":"a\u001bb","note":{"from":"x"}}`, apperr.ForbiddenControlChar, ""},
		{"全文が 8000 字超", `{"pane":"T1","agent":"omp","profile":"fix","task":"` + strings.Repeat("あ", 8000) + `","note":{"from":"x"}}`, apperr.TextTooLong, ""},
		{"command の改行", `{"pane":"T1","agent":"omp","profile":"fix","task":"x","command":"/a\n/b"}`, apperr.ForbiddenControlChar, "command"},
		{"extra_rules の改行", `{"pane":"T1","agent":"omp","profile":"fix","task":"x","extra_rules":["a\nb"]}`, apperr.ForbiddenControlChar, "extra_rules"},
		{"shell に cwd なし", `{"pane":"B1",` + dispatchOK + `,"title":"t","note":{"from":"x"}}`, apperr.InvalidArgs, "cwd"},
		{"shell に title なし", `{"pane":"B1",` + dispatchOK + `,"cwd":"` + f.Root + `","note":{"from":"x"}}`, apperr.InvalidArgs, "title"},
		{"許可外の cwd", `{"pane":"B1",` + dispatchOK + `,"cwd":"/etc","title":"t","note":{"from":"x"}}`, apperr.ForbiddenPath, "cwd"},
		{"存在しないペイン", `{"pane":"Z9",` + dispatchOK + `,"note":{"from":"x"}}`, apperr.PaneNotFound, ""},
	}
	for _, c := range cases {
		_, err := call(t, f, "dispatch", c.args)
		d := wantCode(t, err, c.code)
		if c.field != "" && d["field"] != c.field {
			t.Errorf("%s: details = %v", c.name, d)
		}
		if strings.Contains(tools.ErrorText(err), secret) {
			t.Errorf("%s: エラー本文に秘密が出ています", c.name)
		}
	}
	if f.Ops.Called() {
		t.Errorf("事前検証で落ちたのに送信が行われました: %v", f.Ops.Calls)
	}
	if ids := f.Notes.ActiveIDs(); len(ids) != 0 {
		t.Errorf("事前検証で落ちたのにメモが保存されました: %v", ids)
	}
}

// Ops が拒否（invalid_state）したときはメモを保存しない。送信後に受理されなかったときは保存する（P-23）。
func TestDispatchNoteSavedOnlyAfterSending(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.Err = apperr.New(apperr.InvalidState, "state", "working")
	_, err := call(t, f, "dispatch", `{"pane":"B2",`+dispatchOK+`,"note":{"from":"x"}}`)
	wantCode(t, err, apperr.InvalidState)
	if ids := f.Notes.ActiveIDs(); len(ids) != 0 {
		t.Fatalf("拒否されたのにメモが保存されました: %v", ids)
	}
	f.Ops.Err = nil
	f.Ops.DispatchRes = pane.DispatchResult{OK: false, State: agent.StateIdle, Accepted: false}
	m := mustCall(t, f, "dispatch", `{"pane":"T1",`+dispatchOK+`,"note":{"from":"x"}}`)
	if m["ok"] != false || m["accepted"] != false || m["note_saved"] != true {
		t.Errorf("受理されなかった dispatch = %v", m)
	}
	if n, ok := f.Notes.Get("%1"); !ok || n.From != "x" {
		t.Errorf("送信後にメモが保存されていません: %+v", n)
	}
}

func TestDispatchKeyOrder(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true}
	text, err := call(t, f, "dispatch", `{"pane":"T1","agent":"omp","task":"x","profile":"fix"}`)
	if err != nil {
		t.Fatal(err)
	}
	prev := -1
	for _, k := range []string{`"ok"`, `"state"`, `"accepted"`, `"started"`, `"sent"`, `"note_saved"`} {
		i := strings.Index(text, k)
		if i <= prev {
			t.Fatalf("キー %s の位置が不正: %s", k, text)
		}
		prev = i
	}
}

// shell のペインで起動に失敗したときは何も送っていないので、sent は空でメモも保存しない。
func TestDispatchShellStartFailureSendsNothing(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: false, State: agent.StateShell, Started: false}
	m := mustCall(t, f, "dispatch", `{"pane":"B1",`+dispatchOK+`,"cwd":"`+f.Root+`","title":"t","note":{"from":"x"}}`)
	if m["ok"] != false || m["sent"] != "" || m["note_saved"] != false || m["started"] != false {
		t.Errorf("dispatch = %v", m)
	}
	if ids := f.Notes.ActiveIDs(); len(ids) != 0 {
		t.Errorf("送っていないのにメモが保存されました: %v", ids)
	}
}
