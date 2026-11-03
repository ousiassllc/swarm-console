package ops

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type paneStartArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
	Agent   *string `json:"agent"`
	Cwd     *string `json:"cwd"`
	Title   *string `json:"title"`
}

type paneStartJSON struct {
	OK    bool   `json:"ok"`
	State string `json:"state"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	Title string `json:"title"`
}

func (e *env) paneStartTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "pane_start",
		Description: descPaneStart,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane", "agent", "cwd", "title"}, toolkit.Props{
			"pane":    toolkit.StrProp("shell のペイン。" + toolkit.PanePropDesc),
			"session": toolkit.SessionProp(),
			"agent":   toolkit.EnumProp("起動するエージェント", agent.Builtin()...),
			"cwd":     toolkit.StrProp("作業フォルダの絶対パス（許可されたフォルダの下の既存ディレクトリ）"),
			"title":   toolkit.StrProp("タイトル（80 字まで、制御文字なし）。設定の接頭辞が付く"),
		}),
		Handler: e.paneStart,
	}
}

func (e *env) paneStart(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a paneStartArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	name, err := checkAgent(a.Agent)
	if err != nil {
		return toolkit.Result{}, err
	}
	cwd, title, err := e.checkStartTarget(a.Cwd, a.Title)
	if err != nil {
		return toolkit.Result{}, err
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.Start(ctx, ref, name, cwd, title)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	return toolkit.MarshalResult(paneStartJSON{OK: res.OK, State: string(res.State), Agent: res.Agent, Cwd: res.Cwd, Title: res.Title})
}

// checkAgent は agent 引数を検証する。起動できるのは omp・claude・codex だけ（FR-SAFE-3）。
func checkAgent(p *string) (string, error) {
	name, err := toolkit.RequireStr(p, "agent")
	if err != nil {
		return "", err
	}
	if !agent.IsBuiltin(name) {
		return "", toolkit.InvalidArg("agent")
	}
	return name, nil
}

// checkStartTarget は pane_start・dispatch（shell のとき）共通の cwd・title を検証する。
// cwd は safety.CheckCWD 済み（シンボリックリンク解決後）の値を返す。
func (e *env) checkStartTarget(cwdArg, titleArg *string) (cwd, title string, err error) {
	if title, err = toolkit.RequireStr(titleArg, "title"); err != nil {
		return "", "", err
	}
	if utf8.RuneCountInString(title) > config.MaxTitleRunes || e.Safety.CheckLine(title, config.MaxTitleRunes) != nil {
		return "", "", toolkit.InvalidArg("title")
	}
	given, err := toolkit.RequireStr(cwdArg, "cwd")
	if err != nil {
		return "", "", err
	}
	if cwd, err = e.Safety.CheckCWD(given); err != nil {
		return "", "", apperr.As(err)
	}
	return cwd, title, nil
}

type paneCloseJSON struct {
	OK           bool     `json:"ok"`
	State        string   `json:"state"`
	Steps        []string `json:"steps"`
	NoteArchived bool     `json:"note_archived"`
}

func (e *env) paneCloseTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "pane_close",
		Description: descPaneClose,
		Scope:       toolkit.ScopeOps,
		Schema:      toolkit.Object([]string{"pane"}, toolkit.Props{"pane": toolkit.PaneProp(), "session": toolkit.SessionProp()}),
		Handler:     e.paneClose,
	}
}

func (e *env) paneClose(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a toolkit.PaneArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.Close(ctx, ref)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	steps := res.Steps
	if steps == nil {
		steps = []string{}
	}
	return toolkit.MarshalResult(paneCloseJSON{OK: res.OK, State: string(res.State), Steps: steps, NoteArchived: res.NoteArchived})
}
