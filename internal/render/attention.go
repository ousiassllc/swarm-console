package render

import (
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// attention の kind（API 仕様 §5.5）。
const (
	kindAsking        = "asking"
	kindStalled       = "stalled"
	kindReportWaiting = "report_waiting"
	kindAuthExpired   = "auth_expired"
	kindAuthExpiring  = "auth_expiring"
)

// attentionItem は要対応 1 件。Detail は言語に依存しない（json の detail）。
// Expires は auth_expiring の期限（markdown の「あと N 時間」用。json には出さない）。
type attentionItem struct {
	Kind    string
	Pane    string
	Detail  string
	Expires time.Time
}

// attentionItems は要対応を「質問待ち → 止まってそう → 報告待ち → ログイン期限切れ → ログイン期限間近」の順に返す。
// ペインの項目は各グループでペインの並び順、ログインの項目は auth の並び順（omp・claude・codex）。
// 報告待ちは idle かつ最終報告ファイルありのペイン（FR-REP-8）。ログインの項目はペインに属さないので、
// panes を 1 ペインに絞っても常に入れる（FR-REP-16。pane は ""、detail はエージェント名）。`unknown` は入れない。
func attentionItems(panes []pane.PaneState, reportDir string, auth []host.Auth) []attentionItem {
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
		func(p *pane.PaneState) string { return pane.ReportPath(reportDir, p.Pos) })
	for _, want := range []struct{ status, kind string }{
		{host.AuthExpired, kindAuthExpired}, {host.AuthExpiringSoon, kindAuthExpiring},
	} {
		for _, a := range auth {
			if a.Status != want.status {
				continue
			}
			it := attentionItem{Kind: want.kind, Detail: a.Agent}
			if a.ExpiresAt != nil {
				it.Expires = *a.ExpiresAt
			}
			out = append(out, it)
		}
	}
	return out
}

// line0 は改行などを空白にした 1 行（空は空のまま）。
func line0(s string) string { return strings.TrimSpace(flatten(s)) }

// attentionText は markdown の要対応 1 行（行頭の「- 」は含まない）。at はスナップショットの時刻（呼び出し時刻は使わない）。
func (l locale) attentionText(a attentionItem, at time.Time) string {
	var label, detail string
	switch a.Kind {
	case kindAsking:
		label, detail = l.asking, a.Detail
	case kindStalled:
		label = l.stalled
		if a.Detail != "" {
			detail = l.noChange + a.Detail
		}
	case kindAuthExpired:
		return l.authExpired + ": " + a.Detail
	case kindAuthExpiring:
		s := l.authExpiring + ": " + a.Detail
		if !a.Expires.IsZero() {
			s += " — " + l.remaining(a.Expires.Sub(at)) + l.paren(a.Expires.In(at.Location()).Format(minuteLayout))
		}
		return s
	default:
		label, detail = l.reportWait, l.reportReady
	}
	s := label + ": " + a.Pane
	if detail != "" {
		s += " — " + detail
	}
	return s
}
