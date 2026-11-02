package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// 数量の上限（API 仕様 §7）。
const (
	tailDefaultLines = 30
	tailMaxLines     = 200
	reportReadMax    = 20000
)

type paneTailArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
	Lines   *int    `json:"lines"`
	Raw     *bool   `json:"raw"`
}

type paneTailJSON struct {
	Pane      string `json:"pane"`
	Lines     int    `json:"lines"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

func (e *env) paneTailTool() Tool {
	return Tool{
		Name:        "pane_tail",
		Description: descPaneTail,
		Scope:       ScopeReport,
		Schema: object([]string{"pane"}, props{
			"pane":    paneProp(),
			"session": sessionProp(),
			"lines":   intProp("最後の行数（既定 30）", 1, tailMaxLines),
			"raw":     boolProp("true なら飾りも残す"),
		}),
		Handler: e.paneTail,
	}
}

func (e *env) paneTail(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a paneTailArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	lines := tailDefaultLines
	if a.Lines != nil {
		lines = *a.Lines
		if lines < 1 || lines > tailMaxLines {
			return Result{}, invalidArg("lines")
		}
	}
	ref, err := e.resolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return Result{}, err
	}
	t, err := e.Registry.Tail(ctx, ref, lines, a.Raw != nil && *a.Raw)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return marshalResult(paneTailJSON{Pane: ref.Local, Lines: lines, Text: t.Text, Truncated: t.Truncated})
}

type paneArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
}

type optionJSON struct {
	N           int    `json:"n"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
}

type askCardJSON struct {
	Asking       bool         `json:"asking"`
	Question     string       `json:"question"`
	Options      []optionJSON `json:"options"`
	Cursor       int          `json:"cursor"`
	AllowsCustom bool         `json:"allows_custom"`
	Truncated    bool         `json:"truncated,omitempty"`
}

func (e *env) askCardTool() Tool {
	return Tool{
		Name:        "ask_card",
		Description: descAskCard,
		Scope:       ScopeReport,
		Schema:      object([]string{"pane"}, props{"pane": paneProp(), "session": sessionProp()}),
		Handler:     e.askCard,
	}
}

func (e *env) askCard(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a paneArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	ref, err := e.resolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return Result{}, err
	}
	card, err := e.Registry.Card(ctx, ref)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	if card == nil {
		return Result{Text: `{"asking":false}`}, nil
	}
	out := askCardJSON{
		Asking: true, Question: card.Question, Options: make([]optionJSON, 0, len(card.Options)),
		Cursor: card.Cursor, AllowsCustom: card.AllowsCustom, Truncated: card.Truncated,
	}
	for _, o := range card.Options {
		out.Options = append(out.Options, optionJSON{N: o.N, Label: o.Label, Recommended: o.Recommended})
	}
	return marshalResult(out)
}

type reportReadJSON struct {
	Exists    bool   `json:"exists"`
	Path      string `json:"path"`
	UpdatedAt string `json:"updated_at"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

func (e *env) reportReadTool() Tool {
	return Tool{
		Name:        "report_read",
		Description: descReportRead,
		Scope:       ScopeReport,
		Schema:      object([]string{"pane"}, props{"pane": paneProp(), "session": sessionProp()}),
		Handler:     e.reportRead,
	}
}

func (e *env) reportRead(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a paneArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	ref, err := e.resolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return Result{}, err
	}
	path := pane.ReportPath(e.Cfg.ReportDir, ref.Pos)
	text, mtime, ok, err := readReportFile(path)
	if err != nil {
		return Result{}, apperr.Wrap(apperr.Internal, err)
	}
	if !ok {
		return Result{Text: `{"exists":false}`}, nil
	}
	out := reportReadJSON{Exists: true, Path: path, UpdatedAt: mtime.Format(time.RFC3339)}
	if utf8.RuneCountInString(text) > reportReadMax {
		text, out.Truncated = string([]rune(text)[:reportReadMax]), true
	}
	out.Text = text
	return marshalResult(out)
}

// readReportFile は最終報告ファイルを読む。通常ファイルでなければ（無い・リンク・ディレクトリ）ok=false。
// エージェントが書く場所なので、リンクをたどって別のファイルを読まない（O_NOFOLLOW）。
func readReportFile(path string) (text string, mtime time.Time, ok bool, err error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0) //nolint:gosec // report_dir 配下の固定形式のパス
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ELOOP) {
			return "", time.Time{}, false, nil
		}
		return "", time.Time{}, false, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return "", time.Time{}, false, err
	}
	if !st.Mode().IsRegular() {
		return "", time.Time{}, false, nil
	}
	// 1 字は UTF-8 で最大 4 バイト。上限字数ぶん＋1 字だけ読めば切り詰めの判定ができる。
	data, err := io.ReadAll(io.LimitReader(f, (reportReadMax+1)*utf8.UTFMax))
	if err != nil {
		return "", time.Time{}, false, err
	}
	return strings.ToValidUTF8(string(data), "\uFFFD"), st.ModTime(), true, nil
}
