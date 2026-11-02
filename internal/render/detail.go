package render

import (
	"strconv"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// メモの項目の並び（ER §2.2）。locale.fields の添字と対応する。
const (
	fFrom = iota
	fReportTo
	fRepo
	fItems
	fWorktree
	fBranch
	fInstruction
	fApproval
	fRules
	fStatus
	fNext
	fResumeID
	fMemo
	fUpdatedAt
	fieldCount
)

type detailView struct {
	Pos, ID, Title, Cwd, Elapsed, ChangedAgo string
	ReportLine                               string
	Note                                     *noteView
	Card                                     *cardView
	TailCount                                int
	Tail                                     string
}

type noteView struct {
	Fields []fieldView
	Log    []string
}

type fieldView struct{ Label, Value string }

type cardView struct {
	Question     string
	Options      []optionView
	Cursor       int
	AllowsCustom string
}

type optionView struct {
	N           int
	Label       string
	Recommended bool
}

func newDetail(in *ReportInput, l locale, p *pane.PaneState) *detailView {
	d := &detailView{
		Pos: line(p.Pos), ID: line(p.ID), Title: line(p.Title), Cwd: line(p.Cwd),
		Elapsed: line(p.Elapsed), ChangedAgo: line(p.ChangedAgo),
		ReportLine: l.missing,
	}
	det := in.Detail
	if det == nil {
		det = &PaneDetail{Report: ReportFile{Exists: p.ReportExists}}
	}
	if det.Report.Exists {
		path := pane.ReportPath(in.ReportDir, p.Pos)
		d.ReportLine = l.exists + "（`" + path + "`"
		if !det.Report.UpdatedAt.IsZero() {
			d.ReportLine += "、" + l.updated + formatTime(det.Report.UpdatedAt, in.Snapshot.At)
		}
		d.ReportLine += "）"
	}
	d.Note = newNoteView(det.Note, l)
	if c := det.Card; c != nil {
		cv := &cardView{Question: line(c.Question), Cursor: c.Cursor, AllowsCustom: l.no}
		if c.AllowsCustom {
			cv.AllowsCustom = l.yes
		}
		for _, o := range c.Options {
			cv.Options = append(cv.Options, optionView{N: o.N, Label: line(o.Label), Recommended: o.Recommended})
		}
		d.Card = cv
	}
	tail := det.Tail
	if len(tail) > tailLines {
		tail = tail[len(tail)-tailLines:]
	}
	d.TailCount = tailLines
	d.Tail = fence(tail)
	return d
}

func newNoteView(n *notes.Note, l locale) *noteView {
	if n == nil {
		return nil
	}
	v := &noteView{}
	add := func(idx int, val string) {
		if val != "" {
			v.Fields = append(v.Fields, fieldView{Label: l.fields[idx], Value: block(val)})
		}
	}
	add(fFrom, n.From)
	if r := n.ReportTo; r != nil {
		add(fReportTo, reportToText(r))
	}
	add(fRepo, n.Repo)
	add(fItems, strings.Join(n.Items, ", "))
	add(fWorktree, n.Worktree)
	add(fBranch, n.Branch)
	add(fInstruction, n.Instruction)
	if a := n.Approval; a != nil {
		add(fApproval, approvalText(a))
	}
	add(fRules, strings.Join(n.Rules, " / "))
	add(fStatus, n.Status)
	add(fNext, n.Next)
	add(fResumeID, n.ResumeID)
	add(fMemo, n.Memo)
	add(fUpdatedAt, n.UpdatedAt)
	for _, e := range n.Log {
		v.Log = append(v.Log, line(e.At+" "+e.Text))
	}
	if len(v.Fields) == 0 && len(v.Log) == 0 {
		return nil
	}
	return v
}

func reportToText(r *notes.ReportTo) string {
	parts := []string{r.Kind}
	for _, s := range []string{r.Name, r.To, r.Subject, r.ReplyToID} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if r.Priority {
		parts = append(parts, "priority")
	}
	return strings.Join(parts, " / ")
}

func approvalText(a *notes.Approval) string {
	s := strconv.Quote(a.Text)
	if a.Via != "" {
		s += " via " + a.Via
	}
	if a.At != "" {
		s += " at " + a.At
	}
	return s
}
