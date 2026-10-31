package render

import (
	"strings"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Report は report ツールの本文を返す。markdown（既定）は FR-REPORT §5.3 のレイアウト、
// json は panes_status と同じペイン配列に要対応（attention）を足したもの（FR-REP-9）。
// Format・Lang が不正なら apperr.InvalidArgs、PaneID が解決できなければ apperr.PaneNotFound。
func Report(in ReportInput) (string, error) {
	if in.Snapshot == nil {
		return "", apperr.New(apperr.Internal)
	}
	lang := in.Lang
	if lang == "" {
		lang = in.DefaultLang
	}
	if lang == "" {
		lang = LangJA
	}
	if _, ok := locales[lang]; !ok {
		return "", apperr.New(apperr.InvalidArgs, "field", "lang")
	}
	format := in.Format
	if format == "" {
		format = FormatMarkdown
	}
	if format != FormatMarkdown && format != FormatJSON {
		return "", apperr.New(apperr.InvalidArgs, "field", "format")
	}
	var target *pane.PaneState
	if in.PaneID != "" {
		p, ok := in.Snapshot.Find(in.PaneID)
		if !ok {
			return "", apperr.New(apperr.PaneNotFound, "pane", in.PaneID)
		}
		target = p
	}
	if format == FormatJSON {
		b, err := reportJSON(&in, target)
		if err != nil {
			return "", apperr.Wrap(apperr.Internal, err)
		}
		return string(b), nil
	}
	if in.Templates == nil {
		t, err := LoadTemplates(in.TemplatesDir)
		if err != nil {
			return "", apperr.Wrap(apperr.Internal, err)
		}
		in.Templates = t
	}
	out, err := renderMarkdown(&in, lang, target)
	if err != nil {
		return "", apperr.Wrap(apperr.Internal, err)
	}
	return out, nil
}

// renderMarkdown は view を作ってテンプレートを描画し、末尾を改行 1 つにそろえる。
func renderMarkdown(in *ReportInput, lang string, target *pane.PaneState) (string, error) {
	out, err := in.Templates.execute(lang, newView(in, locales[lang], target))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n") + "\n", nil
}
