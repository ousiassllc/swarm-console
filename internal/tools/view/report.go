package view

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/session"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

// detailTailLines は pane 指定の詳しい版に出す画面の行数（機能要件 §5.3）。
const detailTailLines = 15

type reportArgs struct {
	Session *string `json:"session"`
	Format  *string `json:"format"`
	Pane    *string `json:"pane"`
	Lang    *string `json:"lang"`
}

func (e *env) reportTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "report",
		Description: descReport,
		Scope:       toolkit.ScopeReport,
		Schema: toolkit.Object(nil, toolkit.Props{
			"session": toolkit.SessionProp(),
			"format":  toolkit.EnumProp("出力形式。markdown（既定）か json", render.FormatMarkdown, render.FormatJSON),
			"pane":    toolkit.StrProp("指定すると 1 ペインの詳しい版。" + toolkit.PanePropDesc),
			"lang":    toolkit.EnumProp("言語。省略は設定の既定", render.LangJA, render.LangEN),
		}),
		Handler: e.report,
	}
}

func (e *env) report(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a reportArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	in := render.ReportInput{Lang: deref(a.Lang), Format: deref(a.Format)}
	if err := checkReportArgs(in); err != nil {
		return toolkit.Result{}, err
	}
	allowed := e.Allowed(c)
	s, err := session.Pick(allowed, e.Cfg.Sessions, a.Session)
	if err != nil {
		return toolkit.Result{}, err
	}
	full, err := e.Registry.Current(ctx)
	if err != nil {
		return toolkit.Result{}, apperr.Wrap(apperr.Internal, err)
	}
	var ref *pane.Ref
	if a.Pane != nil && *a.Pane != "" {
		r, err := session.Resolve(full, allowed, s, *a.Pane)
		if err != nil {
			return toolkit.Result{}, err
		}
		ref = &r
	}
	text, err := e.renderReport(ctx, in, full.ForSession(s), ref)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	return toolkit.Result{Text: text}, nil
}

// checkReportArgs は format・lang を、画面を読みに行く前に確かめる。空は既定値。
func checkReportArgs(in render.ReportInput) error {
	switch in.Format {
	case "", render.FormatMarkdown, render.FormatJSON:
	default:
		return toolkit.InvalidArg("format")
	}
	switch in.Lang {
	case "", render.LangJA, render.LangEN:
	default:
		return toolkit.InvalidArg("lang")
	}
	return nil
}

// renderReport は snap（1 セッションに絞ったスナップショット）から report を作る。resource（swarm://report・swarm://pane/…）も使う。
// ref が nil なら全体、あればその 1 ペインの詳しい版。
func (e *env) renderReport(ctx context.Context, in render.ReportInput, snap *pane.Snapshot, ref *pane.Ref) (string, error) {
	in.Snapshot = snap
	in.Templates = e.Templates
	in.DefaultLang = e.Cfg.DefaultLang
	in.ReportDir = e.Cfg.ReportDir
	if ref != nil {
		in.PaneID = ref.ID
		if in.Format != render.FormatJSON {
			var err error
			if in.Detail, err = e.paneDetail(ctx, snap, *ref); err != nil {
				return "", err
			}
		}
	}
	return render.Report(in)
}

// paneDetail は 1 ペインの詳しい版に足す情報を集める（メモ・質問カード・画面の末尾・最終報告ファイル）。
func (e *env) paneDetail(ctx context.Context, snap *pane.Snapshot, ref pane.Ref) (*render.PaneDetail, error) {
	d := &render.PaneDetail{}
	if n, ok := e.Notes.Get(ref.ID); ok {
		d.Note = n
	}
	if p, ok := snap.Find(ref.ID); ok && p.State == agent.StateAsking {
		card, err := e.Registry.Card(ctx, ref)
		if err != nil && apperr.As(err).Code != apperr.UnsupportedAgent {
			return nil, err
		}
		d.Card = card
	}
	tail, err := e.Registry.Tail(ctx, ref, detailTailLines, false)
	if err != nil {
		return nil, err
	}
	if tail.Text != "" {
		d.Tail = strings.Split(tail.Text, "\n")
	}
	// リンクは辿らず「無し」（P-30。report_read と同じ。リンク先の更新時刻を見せない）。
	if fi, err := os.Lstat(pane.ReportPath(e.Cfg.ReportDir, ref.Pos)); err == nil && fi.Mode().IsRegular() {
		d.Report = render.ReportFile{Exists: true, UpdatedAt: fi.ModTime()}
	}
	return d, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
