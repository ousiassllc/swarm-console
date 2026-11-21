package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// args を、擬似エージェントが受け取った起動時の旗（ARGS）に直す。
func startFlags(t *testing.T, h *harness.H, id string) []string {
	t.Helper()
	ev := h.WaitEvents(id, "起動時の旗", func(ev []fakeEvent) bool { return len(ev) >= 1 })
	if ev[0].Kind != "ARGS" {
		t.Fatalf("--log の先頭が ARGS でない: %+v", ev)
	}
	return strings.Split(ev[0].Text, "\n")
}

// pane_start：agent ごとの旗が、実際の tmux のシェルに 1 語ずつ届く。メモ・panes_status・report に model・thinking が出る。
func TestPaneStartModelThinking(t *testing.T) {
	cases := []struct {
		agent, model, thinking string
		flags                  []string
		line                   string
	}{
		{"omp", "anthropic/claude-opus-5-5", "high", []string{"--model=anthropic/claude-opus-5-5", "--thinking=high"}, " --model=anthropic/claude-opus-5-5 --thinking=high"},
		{"claude", "claude-opus-5-5", "xhigh", []string{"--model=claude-opus-5-5", "--effort=xhigh"}, " --model=claude-opus-5-5 --effort=xhigh"},
		{"codex", "gpt-5.4", "", []string{"--model=gpt-5.4"}, " --model=gpt-5.4"},
	}
	h := harness.Start(t, harness.Options{})
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			id := h.NewShell()
			args := map[string]any{"pane": id, "agent": c.agent, "cwd": h.Work, "title": "モデル " + c.agent, "model": c.model}
			if c.thinking != "" {
				args["thinking"] = c.thinking
			}
			res := h.Ops().Map("pane_start", args)
			startCommand, _ := res["start_command"].(string)
			if res["ok"] != true || res["model"] != c.model || !strings.HasPrefix(startCommand, c.agent+" idle --log ") || !strings.HasSuffix(startCommand, c.line) {
				t.Fatalf("pane_start = %v", res)
			}
			if c.thinking != "" && res["thinking"] != c.thinking {
				t.Errorf("thinking = %v", res["thinking"])
			}
			if got := startFlags(t, h, id); !reflect.DeepEqual(got, c.flags) {
				t.Errorf("擬似エージェントが受け取った旗 = %v, want %v", got, c.flags)
			}
			note := h.Report().Map("note_get", map[string]any{"pane": id})["note"].(map[string]any)
			if note["model"] != c.model || (c.thinking != "" && note["thinking"] != c.thinking) {
				t.Errorf("note = %v", note)
			}
			// panes_status・report は snapshot_ttl までの写しなので、メモが載るまで待つ。
			h.Eventually("panes_status に model・thinking が載る", func() bool {
				p, _ := h.Pane(id)
				return p.Model == c.model && p.Thinking == c.thinking
			})
			h.Eventually("report の表に model が載る", func() bool {
				md := h.Report().Call("report", map[string]any{"lang": "en"}).Text
				return strings.Contains(md, "| Model | Thinking |") && strings.Contains(md, c.model)
			})
		})
	}
}

// 指定なしの pane_start は、これまでどおり旗なしで起動する（--log に ARGS が出ない）。
func TestPaneStartWithoutModelHasNoFlags(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewShell()
	res := h.Ops().Map("pane_start", map[string]any{"pane": id, "agent": "omp", "cwd": h.Work, "title": "旗なし"})
	for _, k := range []string{"model", "thinking", "start_command", "note_saved"} {
		if _, has := res[k]; has {
			t.Errorf("%s が出ている: %v", k, res)
		}
	}
	if ev := h.Events(id); len(ev) != 0 {
		t.Errorf("--log = %+v", ev)
	}
	if note := h.Report().Map("note_get", map[string]any{"pane": id})["note"]; note != nil {
		t.Errorf("メモができた: %v", note)
	}
}

// dispatch：shell なら旗付きで起動し、idle の起動済みペインへの指定は何も送らずに拒否する。
func TestDispatchModelThinking(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewShell()
	args := dispatchArgs(id)
	args["cwd"], args["title"] = h.Work, "モデル dispatch"
	args["model"], args["thinking"] = "@slow", "low"
	res := h.Ops().Map("dispatch", args)
	if res["ok"] != true || res["started"] != true || res["model"] != "@slow" || res["thinking"] != "low" {
		t.Fatalf("dispatch = %v", res)
	}
	if line, _ := res["start_command"].(string); !strings.HasSuffix(line, " --model=@slow --thinking=low") {
		t.Errorf("start_command = %q", line)
	}
	ev := h.WaitEvents(id, "旗と貼り付け", func(ev []fakeEvent) bool { return len(ev) >= 3 })
	if !reflect.DeepEqual(harness.Kinds(ev), []string{"ARGS", "PASTE", "SUBMIT"}) || ev[0].Text != "--model=@slow\n--thinking=low" {
		t.Errorf("--log = %+v", ev)
	}
	note := h.Report().Map("note_get", map[string]any{"pane": id})["note"].(map[string]any)
	if note["from"] != "agent-b" || note["model"] != "@slow" || note["thinking"] != "low" {
		t.Errorf("note = %v", note)
	}

	// 監査ログは本文を持たない。起動行（start_command）も model・thinking の値も残さない。
	if log := h.AuditLog(); strings.Contains(log, "--model=") || strings.Contains(log, "@slow") || strings.Contains(log, "start_command") {
		t.Errorf("監査ログに起動時の指定が出ている:\n%s", log)
	}

	idle := h.NewAgent("omp", "idle")
	h.WaitState(idle, "idle")
	for _, extra := range []map[string]any{{"model": "m"}, {"thinking": "high"}, {"model": "m", "thinking": "high"}} {
		a := map[string]any{"pane": idle, "agent": "omp", "task": "x", "profile": "fix", "note": map[string]any{"from": "x"}}
		for k, v := range extra {
			a[k] = v
		}
		r := h.Ops().Call("dispatch", a)
		if r.Code(t) != string(apperr.StartOptionsNA) || r.Err(t).Error.Message != apperr.Message(apperr.StartOptionsNA) {
			t.Errorf("idle + %v: %s", extra, r.Text)
		}
	}
	if ev := h.Events(idle); len(ev) != 0 {
		t.Errorf("idle に何か届いた: %+v", ev)
	}
	if note := h.Report().Map("note_get", map[string]any{"pane": idle})["note"]; note != nil {
		t.Errorf("idle のメモが更新された: %v", note)
	}
}

// 注入の試み：シェルに届く前に拒否し、ペインは shell のまま・何も実行されない。
func TestStartOptionInjectionNeverReachesShell(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	shell := h.NewShell()
	pwned := filepath.Join(h.Work, "pwned")
	attacks := []string{
		"x; touch " + pwned, "x && touch " + pwned, "x\ntouch " + pwned, "$(touch " + pwned + ")", "`touch " + pwned + "`",
		"x > " + pwned, "x | tee " + pwned, "x'; touch " + pwned + "; '", "x\" ; touch " + pwned,
	}
	for _, tool := range []string{"pane_start", "dispatch"} {
		for _, attack := range attacks {
			for field, code := range map[string]apperr.Code{"model": apperr.InvalidModel, "thinking": apperr.InvalidThinking} {
				args := map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work, "title": "t", field: attack}
				if tool == "dispatch" {
					args["task"], args["profile"] = "x", "fix"
				}
				if got := h.Ops().Call(tool, args).Code(t); got != string(code) {
					t.Errorf("%s %s=%q: code = %s, want %s", tool, field, attack, got, code)
				}
			}
		}
	}
	h.Refresh(shell)
	if p, _ := h.Pane(shell); p.State != "shell" {
		t.Errorf("ペインが %s になった", p.State)
	}
	if _, err := os.Stat(pwned); err == nil {
		t.Fatal("注入したコマンドが実行された")
	}
	if ev := h.Events(shell); len(ev) != 0 {
		t.Errorf("擬似エージェントが起動した: %+v", ev)
	}
}

// 拒否の表（エラーコード・固定メッセージ・details.field が、2 つのクライアントで同じ）。ペインは変わらない。
func TestStartOptionRejections(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	shell := h.NewShell()
	long := strings.Repeat("a", 101)
	checkRejections(t, h, []rejection{
		{"model が空", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work, "title": "t", "model": ""}, apperr.InvalidModel, "model"},
		{"model に空白", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work, "title": "t", "model": "a b"}, apperr.InvalidModel, "model"},
		{"model が長い", "pane_start", map[string]any{"pane": shell, "agent": "claude", "cwd": h.Work, "title": "t", "model": long}, apperr.InvalidModel, "model"},
		{"thinking が空", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work, "title": "t", "thinking": ""}, apperr.InvalidThinking, "thinking"},
		{"thinking が許可リスト外", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work, "title": "t", "thinking": "High"}, apperr.InvalidThinking, "thinking"},
		{"claude で off", "pane_start", map[string]any{"pane": shell, "agent": "claude", "cwd": h.Work, "title": "t", "thinking": "off"}, apperr.InvalidThinking, "thinking"},
		{"codex で thinking", "pane_start", map[string]any{"pane": shell, "agent": "codex", "cwd": h.Work, "title": "t", "thinking": "high"}, apperr.ThinkingNA, "thinking"},
		{"dispatch で claude の off", "dispatch", map[string]any{"pane": shell, "agent": "claude", "cwd": h.Work, "title": "t", "task": "x", "profile": "fix", "thinking": "minimal"}, apperr.InvalidThinking, "thinking"},
		{"dispatch で codex の thinking", "dispatch", map[string]any{"pane": shell, "agent": "codex", "cwd": h.Work, "title": "t", "task": "x", "profile": "fix", "thinking": "low"}, apperr.ThinkingNA, "thinking"},
	})
	h.Refresh(shell)
	if p, _ := h.Pane(shell); p.State != "shell" {
		t.Errorf("拒否のあとにペインが %s", p.State)
	}
	if ev := h.Events(shell); len(ev) != 0 {
		t.Errorf("擬似エージェントが起動した: %+v", ev)
	}
	if note := h.Ops().Map("note_get", map[string]any{"pane": shell})["note"]; note != nil {
		t.Errorf("メモが保存された: %v", note)
	}
}
