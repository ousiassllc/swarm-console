package startoptstests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

func workDir(t *testing.T, f *toolstest.Fixture) string {
	t.Helper()
	cwd := filepath.Join(f.Root, "work")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	return cwd
}

// pane_start：model・thinking は検査して Ops に渡し、実際の起動行・指定値を応答に返し、メモに書く。
func TestPaneStartWithModelAndThinking(t *testing.T) {
	f := toolstest.New(t)
	cwd := workDir(t, f)
	line := "omp --model=anthropic/claude-opus-5-5 --thinking=high"
	f.Ops.StartRes = pane.StartResult{OK: true, State: agent.StateIdle, Agent: "omp", Cwd: cwd, Title: "agent: t", StartCommand: line}
	m := mustCall(t, f, "pane_start", `{"pane":"B1","agent":"omp","cwd":"`+cwd+`","title":"t","model":"anthropic/claude-opus-5-5","thinking":"high"}`)
	if m["model"] != "anthropic/claude-opus-5-5" || m["thinking"] != "high" || m["start_command"] != line || m["note_saved"] != true {
		t.Fatalf("pane_start = %v", m)
	}
	if want := (agent.StartOptions{Model: "anthropic/claude-opus-5-5", Thinking: "high"}); f.Ops.LastStart != want {
		t.Errorf("Ops への指定 = %+v", f.Ops.LastStart)
	}
	if n, ok := f.Notes.Get("%3"); !ok || n.Model != "anthropic/claude-opus-5-5" || n.Thinking != "high" {
		t.Errorf("メモ = %+v", n)
	}
}

// 指定なし（省略・null）は、応答にもメモにも model・thinking を出さない。
func TestPaneStartWithoutOptionsAddsNothing(t *testing.T) {
	f := toolstest.New(t)
	cwd := workDir(t, f)
	f.Ops.StartRes = pane.StartResult{OK: true, State: agent.StateIdle, Agent: "omp", Cwd: cwd, Title: "agent: t"}
	for _, extra := range []string{``, `,"model":null,"thinking":null`} {
		text, err := call(t, f, "pane_start", `{"pane":"B1","agent":"omp","cwd":"`+cwd+`","title":"t"`+extra+`}`)
		want := `{"ok":true,"state":"idle","agent":"omp","cwd":"` + cwd + `","title":"agent: t"}`
		if err != nil || text != want {
			t.Fatalf("pane_start%s = %s (%v)", extra, text, err)
		}
	}
	if !f.Ops.LastStart.IsZero() {
		t.Errorf("Ops への指定 = %+v", f.Ops.LastStart)
	}
	if _, ok := f.Notes.Get("%3"); ok {
		t.Error("メモを作った")
	}
}

// 起動を確かめられなかったとき（ok: false）は、送った行は返すが、メモには書かない。
func TestPaneStartNotOKDoesNotSaveNote(t *testing.T) {
	f := toolstest.New(t)
	cwd := workDir(t, f)
	f.Ops.StartRes = pane.StartResult{OK: false, State: agent.StateShell, Agent: "omp", Cwd: cwd, Title: "agent: t", StartCommand: "omp --model=m"}
	m := mustCall(t, f, "pane_start", `{"pane":"B1","agent":"omp","cwd":"`+cwd+`","title":"t","model":"m"}`)
	if m["start_command"] != "omp --model=m" || m["note_saved"] != nil {
		t.Fatalf("pane_start = %v", m)
	}
	if _, ok := f.Notes.Get("%3"); ok {
		t.Error("起動に失敗したのにメモを書いた")
	}
}

// 不正な指定は、何も呼ばず（ペインへ何も送らず）、メモも書かない。
func TestStartOptionRejections(t *testing.T) {
	f := toolstest.New(t)
	cwd := workDir(t, f)
	f.Ops.StartRes = pane.StartResult{OK: true}
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, Sent: true}
	injection := "x; touch /tmp/pwned"
	cases := []struct {
		name, agent, extra string
		code               apperr.Code
		field              string
	}{
		{"model が空", "omp", `"model":""`, apperr.InvalidModel, "model"},
		{"model に空白", "omp", `"model":"a b"`, apperr.InvalidModel, "model"},
		{"model にコマンド注入", "omp", `"model":"` + injection + `"`, apperr.InvalidModel, "model"},
		{"model に $()", "claude", `"model":"$(id)"`, apperr.InvalidModel, "model"},
		{"model に改行", "codex", `"model":"a\nb"`, apperr.InvalidModel, "model"},
		{"model が長い", "omp", `"model":"` + strings.Repeat("a", 101) + `"`, apperr.InvalidModel, "model"},
		{"model が文字列でない", "omp", `"model":1`, apperr.InvalidArgs, ""},
		{"thinking が空", "omp", `"thinking":""`, apperr.InvalidThinking, "thinking"},
		{"thinking が大文字", "omp", `"thinking":"High"`, apperr.InvalidThinking, "thinking"},
		{"thinking が許可リスト外", "omp", `"thinking":"ultracode"`, apperr.InvalidThinking, "thinking"},
		{"thinking にコマンド注入", "omp", `"thinking":"high; id"`, apperr.InvalidThinking, "thinking"},
		{"claude で off", "claude", `"thinking":"off"`, apperr.InvalidThinking, "thinking"},
		{"claude で minimal", "claude", `"thinking":"minimal"`, apperr.InvalidThinking, "thinking"},
		{"claude で auto", "claude", `"thinking":"auto"`, apperr.InvalidThinking, "thinking"},
		{"codex で thinking", "codex", `"thinking":"high"`, apperr.ThinkingNA, "thinking"},
	}
	for _, c := range cases {
		start := `{"pane":"B1","agent":"` + c.agent + `","cwd":"` + cwd + `","title":"t",` + c.extra + `}`
		disp := `{"pane":"B1","agent":"` + c.agent + `","task":"x","profile":"fix","cwd":"` + cwd + `","title":"t","note":{"from":"x"},` + c.extra + `}`
		for tool, args := range map[string]string{"pane_start": start, "dispatch": disp} {
			_, err := call(t, f, tool, args)
			d := wantCode(t, err, c.code)
			if c.field != "" && d["field"] != c.field {
				t.Errorf("%s %s: details = %v", tool, c.name, d)
			}
		}
	}
	if f.Ops.Called() {
		t.Errorf("拒否したのに操作が呼ばれた: %v", f.Ops.Calls)
	}
	if _, ok := f.Notes.Get("%3"); ok {
		t.Error("拒否したのにメモを書いた")
	}
}

// dispatch（shell から起動）：旗を Ops へ渡し、起動行・指定値を応答に返し、note と合わせてメモに書く。起動時の指定が note の同じ項目に勝つ。
func TestDispatchShellWithModelAndThinking(t *testing.T) {
	f := toolstest.New(t)
	cwd := workDir(t, f)
	line := "claude --model=claude-opus-5-5 --effort=high"
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Started: true, Sent: true, StartCommand: line}
	m := mustCall(t, f, "dispatch", `{"pane":"B1","agent":"claude","task":"#982 を直す","profile":"fix","cwd":"`+cwd+`","title":"t",`+
		`"model":"claude-opus-5-5","thinking":"high","note":{"from":"agent-b","model":"stale"}}`)
	if m["started"] != true || m["model"] != "claude-opus-5-5" || m["thinking"] != "high" || m["start_command"] != line || m["note_saved"] != true {
		t.Fatalf("dispatch = %v", m)
	}
	if got := f.Ops.LastDispatch.Start; got != (agent.StartOptions{Model: "claude-opus-5-5", Thinking: "high"}) {
		t.Errorf("Ops への指定 = %+v", got)
	}
	if n, ok := f.Notes.Get("%3"); !ok || n.From != "agent-b" || n.Model != "claude-opus-5-5" || n.Thinking != "high" {
		t.Errorf("メモ = %+v", n)
	}
}

// 指定なしの dispatch は、応答に model・thinking・start_command を出さず、メモにも書かない。
func TestDispatchWithoutOptionsAddsNothing(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Sent: true}
	m := mustCall(t, f, "dispatch", `{"pane":"T1",`+dispatchOK+`,"model":null,"thinking":null}`)
	for _, k := range []string{"model", "thinking", "start_command"} {
		if _, has := m[k]; has {
			t.Errorf("%s が出ている: %v", k, m)
		}
	}
	if !f.Ops.LastDispatch.Start.IsZero() {
		t.Errorf("Ops への指定 = %+v", f.Ops.LastDispatch.Start)
	}
	if _, ok := f.Notes.Get("%1"); ok {
		t.Error("メモを作った")
	}
}

// note_set でも model・thinking は同じ検査を通った値だけ書ける。null・空文字で消せる。
func TestNoteSetModelThinking(t *testing.T) {
	f := toolstest.New(t)
	m := mustCall(t, f, "note_set", `{"pane":"T1","set":{"model":"@slow","thinking":"xhigh"}}`)
	if n, _ := m["note"].(map[string]any); n["model"] != "@slow" || n["thinking"] != "xhigh" {
		t.Fatalf("note_set = %v", m)
	}
	for _, args := range []string{
		`{"pane":"T1","set":{"model":"a b"}}`,
		`{"pane":"T1","set":{"model":1}}`,
		`{"pane":"T1","set":{"thinking":"High"}}`,
		`{"pane":"T1","set":{"thinking":"max; id"}}`,
	} {
		_, err := call(t, f, "note_set", args)
		wantCode(t, err, apperr.InvalidArgs)
	}
	mustCall(t, f, "note_set", `{"pane":"T1","set":{"model":null,"thinking":""}}`)
	n, _ := mustCall(t, f, "note_get", `{"pane":"T1"}`)["note"].(map[string]any)
	if _, has := n["model"]; has {
		t.Errorf("model が残った: %v", n)
	}
	if _, has := n["thinking"]; has {
		t.Errorf("thinking が残った: %v", n)
	}
}
