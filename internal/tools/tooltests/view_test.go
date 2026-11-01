package tooltests_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

func TestPaneTail(t *testing.T) {
	f := toolstest.New(t)
	f.Registry.DefaultTail = pane.Tail{Text: "a\nb", Truncated: true}
	text, err := call(t, f, "pane_tail", `{"pane":"B2"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"pane":"B2","lines":30,"text":"a\nb","truncated":true}`; text != want {
		t.Errorf("pane_tail = %s, want %s", text, want)
	}
	if f.Registry.TailLines != 30 || f.Registry.TailRaw {
		t.Errorf("既定の呼び出し = lines %d raw %v", f.Registry.TailLines, f.Registry.TailRaw)
	}
	mustCall(t, f, "pane_tail", `{"pane":"B2","lines":200,"raw":true}`)
	if f.Registry.TailLines != 200 || !f.Registry.TailRaw {
		t.Errorf("lines/raw が渡っていません: %d %v", f.Registry.TailLines, f.Registry.TailRaw)
	}
}

func TestAskCard(t *testing.T) {
	f := toolstest.New(t)
	if text, _ := call(t, f, "ask_card", `{"pane":"T1"}`); text != `{"asking":false}` {
		t.Errorf("質問中でないペイン = %s", text)
	}
	f.Registry.Cards = map[string]*agent.Card{"%2": {
		Question: "どうする？", Cursor: 1, AllowsCustom: true,
		Options: []agent.Option{{N: 1, Label: "A", Recommended: true}, {N: 2, Label: "B"}},
	}}
	text, err := call(t, f, "ask_card", `{"pane":"T2"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"asking":true,"question":"どうする？","options":[{"n":1,"label":"A","recommended":true},{"n":2,"label":"B"}],"cursor":1,"allows_custom":true}`
	if text != want {
		t.Errorf("ask_card = %s\nwant %s", text, want)
	}
	_, err = call(t, f, "ask_card", `{"pane":"B3"}`)
	wantCode(t, err, apperr.UnsupportedAgent)
}

func TestReportRead(t *testing.T) {
	f := toolstest.New(t)
	if text, _ := call(t, f, "report_read", `{"pane":"T1"}`); text != `{"exists":false}` {
		t.Errorf("ファイルなし = %s", text)
	}
	path := filepath.Join(f.Cfg.ReportDir, "T1.md")
	body := strings.Repeat("あ", 20001)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m := mustCall(t, f, "report_read", `{"pane":"T1"}`)
	if m["exists"] != true || m["path"] != path || m["truncated"] != true {
		t.Errorf("report_read = %v", m)
	}
	if got := []rune(m["text"].(string)); len(got) != 20000 {
		t.Errorf("本文の字数 = %d, want 20000", len(got))
	}
	if _, err := time.Parse(time.RFC3339, m["updated_at"].(string)); err != nil {
		t.Errorf("updated_at が RFC 3339 ではありません: %v", err)
	}
}

// 位置の「:」はファイル名で「_」。エージェントが置いたリンクをたどって別のファイルを読まない。
func TestReportReadPathRules(t *testing.T) {
	f := toolstest.New(t)
	f.Registry.Snap.Panes[0].Pos = "work:T1"
	if err := os.WriteFile(filepath.Join(f.Cfg.ReportDir, "work_T1.md"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m := mustCall(t, f, "report_read", `{"pane":"work:T1"}`); m["text"] != "ok" || m["truncated"] != false {
		t.Errorf("work:T1 → %v", m)
	}
	secret := filepath.Join(f.Root, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(f.Cfg.ReportDir, "T2.md")); err != nil {
		t.Fatal(err)
	}
	if text, _ := call(t, f, "report_read", `{"pane":"T2"}`); text != `{"exists":false}` {
		t.Errorf("リンクを読んでしまいました: %s", text)
	}
}

func TestHostStatus(t *testing.T) {
	f := toolstest.New(t)
	text, err := call(t, f, "host_status", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"load":[98.2,95.1,90.4],"cpus":32,"mem_total_gb":128,"mem_free_gb":41.3,` +
		`"disk":[{"mount":"/","free_gb":120},{"mount":"/mnt/nvme","free_gb":830}],` +
		`"tmux_session":true,"agents":{"omp":2,"claude":1,"codex":0},"uptime":"12d 4h"}`
	if text != want {
		t.Errorf("host_status =\n%s\nwant\n%s", text, want)
	}
}

func TestAgentAuthStatus(t *testing.T) {
	f := toolstest.New(t)
	exp := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	f.Host.Auth = map[string]host.Auth{"claude": {Agent: "claude", Status: host.AuthValid, ExpiresAt: &exp}}
	text, err := call(t, f, "agent_auth_status", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, `{"agents":[{"agent":"omp","status":"unknown","expires_at":null},{"agent":"claude","status":"valid","expires_at":"2026-11-`) ||
		!strings.HasSuffix(text, `{"agent":"codex","status":"unknown","expires_at":null}]}`) {
		t.Errorf("agent_auth_status = %s", text)
	}
}

func TestPanesStatusSessionFilter(t *testing.T) {
	f := toolstest.New(t)
	f.Cfg.Sessions = []string{"0", "w"}
	f.Registry.Snap.Panes[1].Session = "w"
	m := mustCall(t, f, "panes_status", `{"session":"w"}`)
	panes := m["panes"].([]any)
	if len(panes) != 1 || panes[0].(map[string]any)["pos"] != "T2" {
		t.Errorf("session=w のペイン = %v", panes)
	}
	if all := mustCall(t, f, "panes_status", `{}`)["panes"].([]any); len(all) != 5 {
		t.Errorf("session 省略のペイン数 = %d", len(all))
	}
}

func TestPanesStatusKeyOrder(t *testing.T) {
	f := toolstest.New(t)
	text, _ := call(t, f, "panes_status", `{}`)
	prev := -1
	for _, k := range []string{`"at"`, `"host"`, `"layout"`, `"panes"`, `"warnings"`} {
		i := strings.Index(text, k)
		if i <= prev {
			t.Fatalf("キー %s の位置が不正 (%d <= %d): %s", k, i, prev, text)
		}
		prev = i
	}
}

// report の markdown・json は render.Report と 1 文字も違わない（同じスナップショットなら同じ出力）。
func TestReportMatchesRender(t *testing.T) {
	f := toolstest.New(t)
	tpl, _ := render.LoadTemplates("")
	for _, c := range []struct{ args, format, lang string }{
		{`{}`, "", ""}, {`{"lang":"en"}`, "", "en"}, {`{"format":"json"}`, "json", ""},
	} {
		want, err := render.Report(render.ReportInput{
			Snapshot: f.Registry.Snap, Templates: tpl, Format: c.format, Lang: c.lang, DefaultLang: "ja", ReportDir: f.Cfg.ReportDir,
		})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if got, err := call(t, f, "report", c.args); err != nil || got != want {
				t.Errorf("report %s が render.Report と違います (err=%v)", c.args, err)
			}
		}
	}
}

func TestReportPaneDetail(t *testing.T) {
	f := toolstest.New(t)
	f.Registry.Tails = map[string]pane.Tail{"%2": {Text: "最後の行ABC"}}
	f.Registry.Cards = map[string]*agent.Card{"%2": {Question: "質問XYZ", Options: []agent.Option{{N: 1, Label: "選択肢A"}}}}
	if _, err := f.Notes.Log(context.Background(), "%2", "ログ行"); err != nil {
		t.Fatal(err)
	}
	text, err := call(t, f, "report", `{"pane":"t2"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"T2", "最後の行ABC", "質問XYZ", "選択肢A", "ログ行"} {
		if !strings.Contains(text, want) {
			t.Errorf("詳しい版に %q がありません:\n%s", want, text)
		}
	}
	if f.Registry.TailLines != 15 || f.Registry.TailRaw {
		t.Errorf("詳しい版の画面 = %d 行 raw=%v, want 15 行 raw=false", f.Registry.TailLines, f.Registry.TailRaw)
	}
	// asking でないペインでは質問カードを取りに行かない（Cards に無いので出ない）。
	if text, err = call(t, f, "report", `{"pane":"T1"}`); err != nil || strings.Contains(text, "質問XYZ") {
		t.Errorf("T1 の詳しい版に他ペインの質問が混ざっています (err=%v)", err)
	}
	_, err = call(t, f, "report", `{"pane":"Z9"}`)
	wantCode(t, err, apperr.PaneNotFound)
}

func TestNoteGetAndHistory(t *testing.T) {
	f := toolstest.New(t)
	if text, _ := call(t, f, "note_get", `{"pane":"T1"}`); text != `{"pane":"T1","id":"%1","note":null}` {
		t.Errorf("メモなし = %s", text)
	}
	mustCall(t, f, "note_set", `{"pane":"T1","set":{"from":"agent-b","repo":"o/r","items":["#1"]}}`)
	m := mustCall(t, f, "note_get", `{"pane":"%1"}`)
	if n, _ := m["note"].(map[string]any); n["from"] != "agent-b" || n["repo"] != "o/r" {
		t.Errorf("note_get = %v", m)
	}
	if _, err := f.Notes.Archive(context.Background(), noteMeta("%1", "T1"), "pane_close"); err != nil {
		t.Fatal(err)
	}
	h := mustCall(t, f, "note_history", `{"repo":"o/r","items":["#1"],"limit":5}`)
	if h["total"] != float64(1) || len(h["entries"].([]any)) != 1 {
		t.Errorf("note_history = %v", h)
	}
	if h := mustCall(t, f, "note_history", `{"repo":"other"}`); h["total"] != float64(0) || len(h["entries"].([]any)) != 0 {
		t.Errorf("絞り込み後が空 = %v", h)
	}
}
