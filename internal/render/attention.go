package render

import (
	"path/filepath"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// attention の kind（API 仕様 §5.5）。
const (
	kindAsking        = "asking"
	kindStalled       = "stalled"
	kindReportWaiting = "report_waiting"
)

// attentionItem は要対応 1 件。Detail は言語に依存しない（json の detail）。
type attentionItem struct {
	Kind   string
	Pane   string
	Detail string
}

// ReportPath は最終報告ファイルのパス（<dir>/<位置>.md。位置の「:」は「_」）。
func ReportPath(dir, pos string) string {
	return filepath.Join(dir, strings.ReplaceAll(pos, ":", "_")+".md")
}

// attentionItems は要対応を「質問待ち → 止まってそう → 報告待ち」の順に、各グループはペインの並び順で返す。
// 報告待ちは idle かつ最終報告ファイルありのペイン（FR-REP-8）。
func attentionItems(panes []pane.PaneState, reportDir string) []attentionItem {
	var out []attentionItem
	add := func(kind string, match func(*pane.PaneState) bool, detail func(*pane.PaneState) string) {
		for i := range panes {
			if p := &panes[i]; match(p) {
				out = append(out, attentionItem{Kind: kind, Pane: p.Pos, Detail: detail(p)})
			}
		}
	}
	add(kindAsking,
		func(p *pane.PaneState) bool { return p.State == agent.StateAsking },
		func(p *pane.PaneState) string { return line0(p.Activity) })
	add(kindStalled,
		func(p *pane.PaneState) bool { return p.State == agent.StateStalled },
		func(p *pane.PaneState) string { return line0(p.ChangedAgo) })
	add(kindReportWaiting,
		func(p *pane.PaneState) bool { return p.State == agent.StateIdle && p.ReportExists },
		func(p *pane.PaneState) string { return ReportPath(reportDir, p.Pos) })
	return out
}

// line0 は改行などを空白にした 1 行（空は空のまま）。
func line0(s string) string { return strings.TrimSpace(flatten(s)) }

// text は markdown の要対応 1 行（行頭の「- 」は含まない）。
func (l locale) attentionText(a attentionItem) string {
	var label, detail string
	switch a.Kind {
	case kindAsking:
		label, detail = l.asking, a.Detail
	case kindStalled:
		label = l.stalled
		if a.Detail != "" {
			detail = l.noChange + a.Detail
		}
	default:
		label, detail = l.reportWait, l.reportReady
	}
	s := label + ": " + a.Pane
	if detail != "" {
		s += " — " + detail
	}
	return s
}
