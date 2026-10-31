package integration

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// reportEnv はスナップショットを固定した（サンプラーなし・ttl 1 分）サーバーと、状態の違う 3 ペイン。
type reportEnv struct {
	h               *harness.H
	ask, work, idle string // ペイン ID
	askPos, workPos string
	idlePos         string
	reportFile      string // idle のペインの最終報告
}

func newReportEnv(t *testing.T) *reportEnv {
	t.Helper()
	h := harness.Start(t, harness.Options{NoSampler: true, Config: map[string]string{"snapshot_ttl": `"1m"`}})
	e := &reportEnv{h: h, ask: h.NewAgent("omp", "ask"), work: h.NewAgent("claude", "working"), idle: h.NewAgent("omp", "idle")}
	pos := func(id string) string {
		idx := strings.TrimSpace(h.TM.Run("display-message", "-p", "-t", id, "#{window_index}"))
		return "w" + idx + ".T1"
	}
	e.askPos, e.workPos, e.idlePos = pos(e.ask), pos(e.work), pos(e.idle)
	h.Ops().Must("note_set", map[string]any{"pane": e.ask, "set": map[string]any{
		"from": "ふみ", "repo": "ousiassllc/funid", "items": []string{"#982"}, "next": "完了したら #983",
	}})
	e.reportFile = pane.ReportPath(h.Reports, e.idlePos)
	h.WriteFile(e.reportFile, "# 最終報告\nPR #1000\n")
	h.Refresh(h.P0) // 準備がすべて済んだ状態でスナップショットを取り直す
	return e
}

func TestReportFormats(t *testing.T) {
	e := newReportEnv(t)
	c := e.h.Report()
	ja := c.Must("report", nil).Text
	en := c.Must("report", map[string]any{"lang": "en"}).Text
	for _, want := range []string{"# swarm-console レポート", "## 要対応", e.askPos, e.workPos, "ふみ", "ousiassllc/funid", "#983"} {
		if !strings.Contains(ja, want) {
			t.Errorf("ja に %q がありません:\n%s", want, ja)
		}
	}
	for _, want := range []string{"# swarm-console report", "## Needs attention", e.askPos, "Asking"} {
		if !strings.Contains(en, want) {
			t.Errorf("en に %q がありません:\n%s", want, en)
		}
	}
	if strings.Contains(ja, "# swarm-console report") || strings.Contains(en, "レポート") {
		t.Error("言語が混ざっています")
	}

	var js struct {
		Panes     []harness.Pane `json:"panes"`
		Attention []struct {
			Kind, Pane, Detail string
		} `json:"attention"`
		Warnings []harness.Warning `json:"warnings"`
	}
	c.Must("report", map[string]any{"format": "json"}).Decode(t, &js)
	if len(js.Panes) != 4 || js.Warnings == nil {
		t.Fatalf("json: panes=%d warnings=%v", len(js.Panes), js.Warnings)
	}
	kinds := map[string]string{}
	for _, a := range js.Attention {
		kinds[a.Kind] = a.Pane
	}
	if kinds["asking"] != e.askPos || kinds["report_waiting"] != e.idlePos {
		t.Errorf("attention = %+v", js.Attention)
	}

	detail := c.Must("report", map[string]any{"pane": e.ask}).Text
	for _, want := range []string{"## 詳細: " + e.askPos, "### 質問カード", "Pick a fruit", "（推奨）", "ふみ"} {
		if !strings.Contains(detail, want) {
			t.Errorf("pane 詳細に %q がありません:\n%s", want, detail)
		}
	}
	if code := c.Call("report", map[string]any{"pane": "ZZ9"}).Code(t); code != "pane_not_found" {
		t.Errorf("存在しない pane: %s", code)
	}
	for _, args := range []map[string]any{{"lang": "fr"}, {"format": "xml"}, {"nope": 1}} {
		if code := c.Call("report", args).Code(t); code != "invalid_args" {
			t.Errorf("%v: %s", args, code)
		}
	}
}

func TestReportRead(t *testing.T) {
	e := newReportEnv(t)
	got := e.h.Report().Map("report_read", map[string]any{"pane": e.idle})
	if got["exists"] != true || got["path"] != e.reportFile || got["text"] != "# 最終報告\nPR #1000\n" || got["truncated"] != false {
		t.Errorf("ファイルあり: %v", got)
	}
	if _, err := time.Parse(time.RFC3339, got["updated_at"].(string)); err != nil {
		t.Errorf("updated_at: %v", err)
	}
	if text := e.h.Report().Must("report_read", map[string]any{"pane": e.work}).Text; text != `{"exists":false}` {
		t.Errorf("ファイルなし: %s", text)
	}
	// シンボリックリンクの最終報告は読まない（存在しない扱い）。
	link := pane.ReportPath(e.h.Reports, e.workPos)
	if err := os.Symlink(e.reportFile, link); err != nil {
		t.Fatal(err)
	}
	if text := e.h.Report().Must("report_read", map[string]any{"pane": e.work}).Text; text != `{"exists":false}` {
		t.Errorf("リンク: %s", text)
	}
}

func TestPromptsAndResources(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	rep, ops := e.h.Report().CS, e.h.Ops().CS

	names := func(cs *mcp.ClientSession) (out []string) {
		res, err := cs.ListPrompts(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range res.Prompts {
			out = append(out, p.Name)
		}
		return out
	}
	if got := names(rep); len(got) != 1 || got[0] != "report" {
		t.Errorf("/report の prompts = %v", got)
	}
	if got := names(ops); len(got) != 2 {
		t.Errorf("/ops の prompts = %v", got)
	}
	p, err := ops.GetPrompt(ctx, &mcp.GetPromptParams{Name: "report", Arguments: map[string]string{"lang": "en"}})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, "lang=en") {
		t.Errorf("report prompt: %v %+v", err, p)
	}
	p, err = ops.GetPrompt(ctx, &mcp.GetPromptParams{Name: "dispatch", Arguments: map[string]string{
		"pane": "T1", "task": "やること", "profile": "fix", "extra_rules": "a\nb",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if text := p.Messages[0].Content.(*mcp.TextContent).Text; !strings.HasSuffix(text, `{"pane":"T1","task":"やること","profile":"fix","extra_rules":["a","b"]}`) {
		t.Errorf("dispatch prompt: %s", text)
	}
	if _, err := rep.GetPrompt(ctx, &mcp.GetPromptParams{Name: "dispatch", Arguments: map[string]string{"task": "x", "profile": "fix"}}); err == nil {
		t.Error("/report で dispatch prompt が引ける")
	}

	read := func(uri string) (string, error) {
		res, err := rep.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			return "", err
		}
		return res.Contents[0].Text, nil
	}
	whole, err := read("swarm://report")
	if err != nil || whole != e.h.Report().Must("report", nil).Text {
		t.Errorf("swarm://report は report ツールと同じ本文のはず: %v", err)
	}
	if text, err := read("swarm://pane/" + url.PathEscape(e.askPos)); err != nil || text != e.h.Report().Must("report", map[string]any{"pane": e.askPos}).Text {
		t.Errorf("swarm://pane/<位置>: %v", err)
	}
	// tmux ID の % は %25 でエスケープする。
	if text, err := read("swarm://pane/" + url.PathEscape(e.ask)); err != nil || !strings.Contains(text, "## 詳細: "+e.askPos) || !strings.Contains(e.ask, "%") {
		t.Errorf("swarm://pane/%%25N: %v\n%s", err, text)
	}
	notes, err := read("swarm://notes/" + url.PathEscape(e.ask))
	if want := e.h.Report().Must("note_get", map[string]any{"pane": e.ask}).Text; err != nil || notes != want {
		t.Errorf("swarm://notes: %v\n%s\n%s", err, notes, want)
	}
	for _, uri := range []string{"swarm://pane/ZZ9", "swarm://notes/ZZ9"} {
		_, err := read(uri)
		var je *jsonrpc.Error
		if !errors.As(err, &je) || je.Code != -32002 || !strings.Contains(string(je.Data), `"pane_not_found"`) {
			t.Errorf("%s: %v", uri, err)
		}
	}
	list, err := rep.ListResources(ctx, nil)
	if err != nil || len(list.Resources) != 1+2*4 {
		t.Errorf("resources/list: %v %d 件", err, len(list.Resources))
	}
}
