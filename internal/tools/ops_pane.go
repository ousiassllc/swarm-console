package tools

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

// maxTitleRunes は title の上限（API 仕様 §7）。
const maxTitleRunes = 80

type paneStartArgs struct {
	Pane  *string `json:"pane"`
	Agent *string `json:"agent"`
	Cwd   *string `json:"cwd"`
	Title *string `json:"title"`
}

type paneStartJSON struct {
	OK    bool   `json:"ok"`
	State string `json:"state"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	Title string `json:"title"`
}

func (e *env) paneStartTool() Tool {
	return Tool{
		Name:        "pane_start",
		Description: descPaneStart,
		Scope:       ScopeOps,
		Schema: object([]string{"pane", "agent", "cwd", "title"}, props{
			"pane":  strProp("shell のペイン。" + panePropDesc),
			"agent": enumProp("起動するエージェント", agent.Builtin()...),
			"cwd":   strProp("作業フォルダの絶対パス（許可されたフォルダの下の既存ディレクトリ）"),
			"title": strProp("タイトル（80 字まで、制御文字なし）。設定の接頭辞が付く"),
		}),
		Handler: e.paneStart,
	}
}

func (e *env) paneStart(ctx context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a paneStartArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	name, err := checkAgent(a.Agent)
	if err != nil {
		return Result{}, err
	}
	cwd, title, err := e.checkStartTarget(a.Cwd, a.Title)
	if err != nil {
		return Result{}, err
	}
	ref, err := resolvePane(ctx, e.Registry, a.Pane)
	if err != nil {
		return Result{}, err
	}
	res, err := e.Ops.Start(ctx, ref, name, cwd, title)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return marshalResult(paneStartJSON{OK: res.OK, State: string(res.State), Agent: res.Agent, Cwd: res.Cwd, Title: res.Title})
}

// checkAgent は agent 引数を検証する。起動できるのは omp・claude・codex だけ（FR-SAFE-3）。
func checkAgent(p *string) (string, error) {
	name, err := requireStr(p, "agent")
	if err != nil {
		return "", err
	}
	if !agent.IsBuiltin(name) {
		return "", invalidArg("agent")
	}
	return name, nil
}

// checkStartTarget は pane_start・dispatch（shell のとき）共通の cwd・title を検証する。
// cwd は safety.CheckCWD 済み（シンボリックリンク解決後）の値を返す。
func (e *env) checkStartTarget(cwdArg, titleArg *string) (cwd, title string, err error) {
	if title, err = requireStr(titleArg, "title"); err != nil {
		return "", "", err
	}
	if utf8.RuneCountInString(title) > maxTitleRunes || e.Safety.CheckLine(title, maxTitleRunes) != nil {
		return "", "", invalidArg("title")
	}
	given, err := requireStr(cwdArg, "cwd")
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

func (e *env) paneCloseTool() Tool {
	return Tool{
		Name:        "pane_close",
		Description: descPaneClose,
		Scope:       ScopeOps,
		Schema:      object([]string{"pane"}, props{"pane": paneProp()}),
		Handler:     e.paneClose,
	}
}

func (e *env) paneClose(ctx context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a paneArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	ref, err := resolvePane(ctx, e.Registry, a.Pane)
	if err != nil {
		return Result{}, err
	}
	res, err := e.Ops.Close(ctx, ref)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	steps := res.Steps
	if steps == nil {
		steps = []string{}
	}
	return marshalResult(paneCloseJSON{OK: res.OK, State: string(res.State), Steps: steps, NoteArchived: res.NoteArchived})
}
