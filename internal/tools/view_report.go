package tools

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
)

// detailTailLines は pane 指定の詳しい版に出す画面の行数（機能要件 §5.3）。
const detailTailLines = 15

type reportArgs struct {
	Format *string `json:"format"`
	Pane   *string `json:"pane"`
	Lang   *string `json:"lang"`
}

func (e *env) reportTool() Tool {
	return Tool{
		Name:        "report",
		Description: descReport,
		Scope:       ScopeReport,
		Schema: object(nil, props{
			"format": enumProp("出力形式。markdown（既定）か json", render.FormatMarkdown, render.FormatJSON),
			"pane":   strProp("指定すると 1 ペインの詳しい版。" + panePropDesc),
			"lang":   enumProp("言語。省略は設定の既定", render.LangJA, render.LangEN),
		}),
		Handler: e.report,
	}
}

func (e *env) report(ctx context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a reportArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	in := render.ReportInput{Lang: deref(a.Lang), Format: deref(a.Format)}
	if err := checkReportArgs(in); err != nil {
		return Result{}, err
	}
	text, err := e.renderReport(ctx, in, a.Pane)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return Result{Text: text}, nil
}

// checkReportArgs は format・lang を、画面を読みに行く前に確かめる。空は既定値。
func checkReportArgs(in render.ReportInput) error {
	switch in.Format {
	case "", render.FormatMarkdown, render.FormatJSON:
	default:
		return invalidArg("format")
	}
	switch in.Lang {
	case "", render.LangJA, render.LangEN:
	default:
		return invalidArg("lang")
	}
	return nil
}

// renderReport は同じスナップショットから report を作る。resource（swarm://report・swarm://pane/…）も使う。
// paneArg が nil か空なら全体。
func (e *env) renderReport(ctx context.Context, in render.ReportInput, paneArg *string) (string, error) {
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return "", apperr.Wrap(apperr.Internal, err)
	}
	in.Snapshot = snap
	in.Templates = e.Templates
	in.DefaultLang = e.Cfg.DefaultLang
	in.ReportDir = e.Cfg.ReportDir
	if paneArg != nil && *paneArg != "" {
		ref, err := e.Registry.Resolve(ctx, *paneArg)
		if err != nil {
			return "", err
		}
		in.PaneID = ref.ID
		if in.Format != render.FormatJSON {
			if in.Detail, err = e.paneDetail(ctx, snap, ref); err != nil {
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
	if fi, err := os.Stat(pane.ReportPath(e.Cfg.ReportDir, ref.Pos)); err == nil && fi.Mode().IsRegular() {
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
