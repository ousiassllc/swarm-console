package tooltests_test

import (
	"errors"
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

func TestAskPick(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.Pick = pane.PickResult{OK: true, State: agent.StateWorking, Picked: &agent.Option{N: 1, Label: "A"}, CardClosed: true}
	text, err := call(t, f, "ask_pick", `{"pane":"T2","n":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"ok":true,"state":"working","picked":{"n":1,"label":"A"},"card_closed":true}`; text != want {
		t.Errorf("ask_pick = %s", text)
	}
	if f.Ops.LastN != 1 || f.Ops.LastText != "" || f.Ops.LastRef.ID != "%2" {
		t.Errorf("Ops への引数 = %+v", f.Ops)
	}
	f.Ops.Pick = pane.PickResult{State: agent.StateAsking}
	if text, _ = call(t, f, "ask_pick", `{"pane":"T2","text":"その他の答え"}`); text != `{"ok":false,"state":"asking","card_closed":false}` {
		t.Errorf("text 指定 = %s", text)
	}
	if f.Ops.LastN != 0 || f.Ops.LastText != "その他の答え" {
		t.Errorf("text が渡っていません: n=%d text=%q", f.Ops.LastN, f.Ops.LastText)
	}
}

func TestAskPickTextRules(t *testing.T) {
	f := toolstest.New(t)
	_, err := call(t, f, "ask_pick", `{"pane":"T2","text":"a\u001bb"}`)
	if d := wantCode(t, err, apperr.ForbiddenControlChar); d["field"] != "text" {
		t.Errorf("details = %v", d)
	}
	_, err = call(t, f, "ask_pick", `{"pane":"T2","text":"`+strings.Repeat("a", 8001)+`"}`)
	wantCode(t, err, apperr.TextTooLong)
	if f.Ops.Called() {
		t.Error("拒否したのに操作が呼ばれました")
	}
}

func TestOpsErrorsPassThrough(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.Err = apperr.New(apperr.InvalidState, "state", "working")
	for name, args := range map[string]string{
		"ask_pick": `{"pane":"B2","n":1}`, "send_text": `{"pane":"B2","text":"x"}`, "send_escape": `{"pane":"B2","confirm":true}`,
		"pane_close": `{"pane":"B2"}`,
	} {
		_, err := call(t, f, name, args)
		if d := wantCode(t, err, apperr.InvalidState); d["state"] != "working" {
			t.Errorf("%s: details = %v", name, d)
		}
	}
	f.Ops.Err = errors.New("tmux が壊れた: secret-xyz")
	_, err := call(t, f, "send_text", `{"pane":"B2","text":"x"}`)
	wantCode(t, err, apperr.Internal)
}

func TestSendText(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.Send = pane.SendResult{OK: true, State: agent.StateWorking, Accepted: true, Chars: 5}
	text, err := call(t, f, "send_text", `{"pane":"T1","text":"a\nb\tc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"ok":true,"state":"working","accepted":true,"chars":5}` || f.Ops.LastText != "a\nb\tc" {
		t.Errorf("send_text = %s / 送った本文 %q", text, f.Ops.LastText)
	}
}

func TestSendTextRejectsBeforeSending(t *testing.T) {
	f := toolstest.New(t)
	for _, c := range []struct {
		args string
		code apperr.Code
	}{
		{`{"pane":"T1","text":"a\u001bb"}`, apperr.ForbiddenControlChar},
		{`{"pane":"T1","text":"a\rb"}`, apperr.ForbiddenControlChar},
		{`{"pane":"T1","text":"` + strings.Repeat("あ", 8001) + `"}`, apperr.TextTooLong},
		{`{"pane":"T1","file":"relative.txt"}`, apperr.InvalidArgs},
		{`{"pane":"T1","file":"/etc/passwd"}`, apperr.ForbiddenPath},
		{`{"pane":"T1","file":"` + filepath.Join(f.Root, "missing.txt") + `"}`, apperr.FileUnreadable},
	} {
		_, err := call(t, f, "send_text", c.args)
		wantCode(t, err, c.code)
	}
	if f.Ops.Called() {
		t.Error("拒否したのに操作が呼ばれました")
	}
}

func TestSendTextFile(t *testing.T) {
	f := toolstest.New(t)
	path := filepath.Join(f.Root, "in.txt")
	if err := os.WriteFile(path, []byte("ファイルの中身\n2 行目"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.Ops.Send = pane.SendResult{OK: true, State: agent.StateWorking, Accepted: true, Chars: 12}
	if _, err := call(t, f, "send_text", `{"pane":"T1","file":"`+path+`"}`); err != nil {
		t.Fatal(err)
	}
	if f.Ops.LastText != "ファイルの中身\n2 行目" {
		t.Errorf("ファイルの中身が渡っていません: %q", f.Ops.LastText)
	}
	bad := filepath.Join(f.Root, "bad.txt")
	if err := os.WriteFile(bad, []byte("a\x1bb"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.Ops.Calls = nil
	_, err := call(t, f, "send_text", `{"pane":"T1","file":"`+bad+`"}`)
	wantCode(t, err, apperr.ForbiddenControlChar)
	empty := filepath.Join(f.Root, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = call(t, f, "send_text", `{"pane":"T1","file":"`+empty+`"}`)
	wantCode(t, err, apperr.FileUnreadable)
	if f.Ops.Called() {
		t.Error("拒否したのに操作が呼ばれました")
	}
}

func TestSendEscapeNeedsConfirm(t *testing.T) {
	f := toolstest.New(t)
	for _, args := range []string{`{"pane":"B2"}`, `{"pane":"B2","confirm":false}`} {
		_, err := call(t, f, "send_escape", args)
		wantCode(t, err, apperr.ConfirmRequired)
	}
	_, err := call(t, f, "send_escape", `{"pane":"B2","confirm":"true"}`)
	wantCode(t, err, apperr.InvalidArgs)
	if f.Ops.Called() {
		t.Error("confirm なしで Esc が送られました")
	}
	f.Ops.EscapeRes = pane.EscapeResult{OK: true, State: agent.StateIdle, Interrupted: true}
	text, err := call(t, f, "send_escape", `{"pane":"B2","confirm":true}`)
	if err != nil || text != `{"ok":true,"state":"idle","interrupted":true}` {
		t.Errorf("send_escape = %s (%v)", text, err)
	}
}

func TestPaneStart(t *testing.T) {
	f := toolstest.New(t)
	cwd := filepath.Join(f.Root, "work")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	f.Ops.StartRes = pane.StartResult{OK: true, State: agent.StateIdle, Agent: "omp", Cwd: cwd, Title: "kana: funid #1"}
	text, err := call(t, f, "pane_start", `{"pane":"B1","agent":"omp","cwd":"`+cwd+`","title":"funid #1"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ok":true,"state":"idle","agent":"omp","cwd":"` + cwd + `","title":"kana: funid #1"}`
	if text != want {
		t.Errorf("pane_start = %s", text)
	}
	if f.Ops.LastAgent != "omp" || f.Ops.LastCwd != cwd || f.Ops.LastTitle != "funid #1" {
		t.Errorf("Ops への引数 = %q %q %q", f.Ops.LastAgent, f.Ops.LastCwd, f.Ops.LastTitle)
	}
}

// cwd は CheckCWD 済み（シンボリックリンク解決後）の値を Ops へ渡す。許可外・不正は何も送らない。
func TestPaneStartCwdRules(t *testing.T) {
	f := toolstest.New(t)
	target := filepath.Join(f.Root, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.Root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	f.Ops.StartRes = pane.StartResult{OK: true, State: agent.StateIdle}
	if _, err := call(t, f, "pane_start", `{"pane":"B1","agent":"claude","cwd":"`+link+`","title":"t"}`); err != nil {
		t.Fatal(err)
	}
	if f.Ops.LastCwd != target {
		t.Errorf("Ops へ渡った cwd = %q, want %q", f.Ops.LastCwd, target)
	}
	f.Ops.Calls = nil
	for _, c := range []struct {
		cwd  string
		code apperr.Code
	}{
		{"/etc", apperr.ForbiddenPath},
		{"/tmp/../etc", apperr.ForbiddenPath},
		{filepath.Join(f.Root, "nope"), apperr.ForbiddenPath},
		{"relative/dir", apperr.InvalidArgs},
		{target + "' ; rm -rf /", apperr.InvalidArgs},
	} {
		_, err := call(t, f, "pane_start", `{"pane":"B1","agent":"omp","title":"t","cwd":"`+c.cwd+`"}`)
		wantCode(t, err, c.code)
	}
	if f.Ops.Called() {
		t.Error("拒否した cwd で操作が呼ばれました")
	}
}

func TestPaneClose(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.CloseRes = pane.CloseResult{OK: true, State: agent.StateShell, Steps: []string{"/exit", "cd ~", "clear", "title"}, NoteArchived: true}
	text, err := call(t, f, "pane_close", `{"pane":"T1"}`)
	if err != nil || text != `{"ok":true,"state":"shell","steps":["/exit","cd ~","clear","title"],"note_archived":true}` {
		t.Errorf("pane_close = %s (%v)", text, err)
	}
	f.Ops.CloseRes = pane.CloseResult{State: agent.StateIdle}
	if text, _ = call(t, f, "pane_close", `{"pane":"T1"}`); text != `{"ok":false,"state":"idle","steps":[],"note_archived":false}` {
		t.Errorf("失敗時の pane_close = %s", text)
	}
}

func TestNoteSetAndLog(t *testing.T) {
	f := toolstest.New(t)
	m := mustCall(t, f, "note_set", `{"pane":"T1","set":{"from":"ふみ","next":"次"}}`)
	if n := m["note"].(map[string]any); m["ok"] != true || n["from"] != "ふみ" || n["next"] != "次" {
		t.Errorf("note_set = %v", m)
	}
	mustCall(t, f, "note_set", `{"pane":"%1","set":{"next":null}}`)
	if n, ok := f.Notes.Get("%1"); !ok || n.From != "ふみ" || n.Next != "" {
		t.Errorf("null で項目が消えていません: %+v", n)
	}
	if m := mustCall(t, f, "note_log", `{"pane":"T1","text":"Ask で B を選んだ"}`); m["log_size"] != float64(1) {
		t.Errorf("note_log = %v", m)
	}
	_, err := call(t, f, "note_log", `{"pane":"T1","text":"a\nb"}`)
	wantCode(t, err, apperr.InvalidArgs)
	_, err = call(t, f, "note_log", `{"pane":"T1","text":"`+strings.Repeat("a", 501)+`"}`)
	wantCode(t, err, apperr.InvalidArgs)
	_, err = call(t, f, "note_set", `{"pane":"T1","set":{"log":[]}}`)
	wantCode(t, err, apperr.InvalidArgs)
	_, err = call(t, f, "note_set", `{"pane":"T1","set":{"nope":"x"}}`)
	wantCode(t, err, apperr.InvalidArgs)
}

// 秘密に見える値は拒否し、その値を応答（エラー本文）にもメモにも出さない。
func TestSecretNeverEchoed(t *testing.T) {
	f := toolstest.New(t)
	secret := "ghp_" + strings.Repeat("A1b2", 8)
	for name, args := range map[string]string{
		"note_set": `{"pane":"T1","set":{"memo":"` + secret + `"}}`,
		"note_log": `{"pane":"T1","text":"` + secret + `"}`,
	} {
		_, err := call(t, f, name, args)
		d := wantCode(t, err, apperr.SecretDetected)
		if text := tools.ErrorText(err); strings.Contains(text, secret) || d["field"] == nil {
			t.Errorf("%s: エラー本文 = %s", name, text)
		}
	}
	if _, ok := f.Notes.Get("%1"); ok {
		t.Error("秘密を拒否したのにメモができています")
	}
}
