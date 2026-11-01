package render

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/pane"
)

// timeLayout は markdown の時刻（サーバーのローカルオフセット付き）。
const timeLayout = "2006-01-02 15:04:05 (-07:00)"

// rfc3339 は RFC 3339 でオフセットを常に数値で出す（UTC でも Z にしない）。
const rfc3339 = "2006-01-02T15:04:05-07:00"

// view はテンプレートに渡す値。文字列はすべて整形・エスケープ済み。
type view struct {
	At        string
	Load1     string
	MemFree   string
	CPU       string // 「42%」または「取得できず」
	Swap      string
	Disk      string
	Updates   string
	Login     string
	Panes     []rowView
	Attention []string
	Usage     usageView
	Warnings  []string
	Detail    *detailView
}

type rowView struct {
	Pos, Agent, State, Elapsed, From, Repo, Activity, Next string
}

// round1 は小数 1 桁に丸める（panes_status の load1・mem_free_gb）。
func round1(f float64) float64 { return math.Round(f*10) / 10 }

func fmt1(f float64) string { return fmt.Sprintf("%.1f", round1(f)) }

func newRow(p *pane.PaneState, l locale) rowView {
	r := rowView{
		Pos: cell(p.Pos), Agent: cell(p.Agent), State: cell(l.stateWord(p.State)), Elapsed: cell(p.Elapsed), Activity: cell(p.Activity),
		From: emptyCell, Repo: emptyCell, Next: emptyCell,
	}
	if n := p.Note; n != nil {
		r.From = cell(n.From)
		r.Repo = cell(strings.TrimSpace(n.Repo + " " + strings.Join(n.Items, ", ")))
		r.Next = cell(n.Next)
	}
	return r
}

// newView は markdown 用の view を作る。target が非 nil なら詳細版（in.Detail を使う）。
// 見出し・要対応・使用状況は target によらず全体の版と同じ（FR-REP §5.3）。
func newView(in *ReportInput, l locale, target *pane.PaneState) *view {
	snap := in.Snapshot
	v := &view{
		At:      snap.At.Format(timeLayout),
		Load1:   fmt1(snap.Host.Load[0]),
		MemFree: fmt1(snap.Host.MemFreeGB),
		CPU:     l.cpuText(snap.Host.CPU),
		Swap:    l.swapText(snap.Host.Swap),
		Disk:    l.diskText(snap.Host.Disk),
		Updates: l.updatesText(snap),
		Login:   l.loginText(snap.Auth),
		Usage:   l.usageView(snap),
	}
	for i := range snap.Panes {
		v.Panes = append(v.Panes, newRow(&snap.Panes[i], l))
	}
	for _, a := range attentionItems(snap.Panes, in.ReportDir, snap.Auth) {
		v.Attention = append(v.Attention, l.attentionText(a, snap.At))
	}
	for _, w := range snap.Warnings {
		msg := line0(w.Message)
		if msg == "" {
			msg = w.Code
		}
		v.Warnings = append(v.Warnings, msg)
	}
	if target != nil {
		v.Detail = newDetail(in, l, target)
	}
	return v
}

// formatTime は t をスナップショットの時刻と同じオフセットで整形する。
func formatTime(t, at time.Time) string {
	return t.In(at.Location()).Format(timeLayout)
}
