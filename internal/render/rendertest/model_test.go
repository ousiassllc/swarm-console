package rendertest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/render"
)

// modelSnapshot は fixedSnapshot の B4 に model・thinking、B5 に model だけを足したもの（ほかのペインは指定なし）。
func modelSnapshot() *render.ReportInput {
	s := fixedSnapshot()
	for i := range s.Panes {
		switch s.Panes[i].Pos {
		case "B4":
			s.Panes[i].Note.Model, s.Panes[i].Note.Thinking = "anthropic/claude-opus-5-5", "high"
		case "work:B5":
			s.Panes[i].Note.Model = "@slow"
		}
	}
	return &render.ReportInput{Snapshot: s}
}

// 起動時の model・thinking は panes_status・report（json・ja・en）に出る。指定のないペインはキーも表のセルも空。
func TestModelAndThinkingInReports(t *testing.T) {
	base := modelSnapshot().Snapshot
	status, err := render.PanesStatusJSON(base)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "panes_status.model.json", string(status)+"\n")
	golden(t, "report.model.json", mustReport(t, render.ReportInput{Snapshot: base, Format: "json"}))
	golden(t, "report.model.ja.md", mustReport(t, render.ReportInput{Snapshot: base, Lang: "ja"}))
	golden(t, "report.model.en.md", mustReport(t, render.ReportInput{Snapshot: base, Lang: "en"}))
}

// panes_status のトップレベルと note に同じ値が出る。指定のないペインには model・thinking のキーがない。
func TestModelKeysOnlyOnPanesThatHaveThem(t *testing.T) {
	b, err := render.PanesStatusJSON(modelSnapshot().Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Panes []map[string]any `json:"panes"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, p := range v.Panes {
		_, topModel := p["model"]
		_, topThinking := p["thinking"]
		note, _ := p["note"].(map[string]any)
		switch p["pos"] {
		case "B4":
			if p["model"] != "anthropic/claude-opus-5-5" || p["thinking"] != "high" || note["model"] != p["model"] || note["thinking"] != p["thinking"] {
				t.Errorf("B4 = %v", p)
			}
		case "work:B5":
			if p["model"] != "@slow" || topThinking || note["model"] != "@slow" || note["thinking"] != nil {
				t.Errorf("B5 = %v", p)
			}
		default:
			if topModel || topThinking || (note != nil && (note["model"] != nil || note["thinking"] != nil)) {
				t.Errorf("指定のない %v に model・thinking が出ている: %v", p["pos"], p)
			}
		}
	}
}

// どのペインにも指定がなければ、表に「モデル」「思考」の列を足さない（既存の表の形のまま）。
func TestNoModelColumnsWithoutModel(t *testing.T) {
	for _, lang := range []string{"ja", "en"} {
		out := mustReport(t, render.ReportInput{Snapshot: fixedSnapshot(), Lang: lang})
		if strings.Contains(out, "モデル") || strings.Contains(out, "Model") || strings.Contains(out, "Thinking") {
			t.Errorf("%s: 指定のない表に列がある:\n%s", lang, out)
		}
	}
}

// report の pane 指定の詳細にも、メモの項目として model・thinking が出る。
func TestModelInPaneDetail(t *testing.T) {
	d := fixedDetail()
	d.Note = &notes.Note{Model: "anthropic/claude-opus-5-5", Thinking: "high", Log: []notes.LogEntry{}}
	for lang, want := range map[string][]string{
		"ja": {"- モデル: anthropic/claude-opus-5-5", "- 思考: high"},
		"en": {"- Model: anthropic/claude-opus-5-5", "- Thinking: high"},
	} {
		out := mustReport(t, render.ReportInput{Snapshot: fixedSnapshot(), Lang: lang, PaneID: "b4", Detail: d})
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: %q がない:\n%s", lang, w, out)
			}
		}
	}
}
