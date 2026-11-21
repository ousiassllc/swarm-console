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
	// Model・Thinking は起動時の指定。nil（省略・null）は旗を付けない。
	Model    *string `json:"model"`
	Thinking *string `json:"thinking"`
}

type paneStartJSON struct {
	OK    bool   `json:"ok"`
	State string `json:"state"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	Title string `json:"title"`
	// 以下は model・thinking を指定して起動したときだけ出す。
	Model        string `json:"model,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	StartCommand string `json:"start_command,omitempty"`
	NoteSaved    *bool  `json:"note_saved,omitempty"`
}

func (e *env) paneStartTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "pane_start",
		Description: descPaneStart,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane", "agent", "cwd", "title"}, toolkit.Props{
			"pane":     toolkit.StrProp("shell のペイン。" + toolkit.PanePropDesc),
			"session":  toolkit.SessionProp(),
			"agent":    toolkit.EnumProp("起動するエージェント", agent.Builtin()...),
			"cwd":      toolkit.StrProp("作業フォルダの絶対パス（許可されたフォルダの下の既存ディレクトリ）"),
			"title":    toolkit.StrProp("タイトル（80 字まで、制御文字なし）。設定の接頭辞が付く"),
			"model":    toolkit.StrProp(modelPropDesc),
			"thinking": toolkit.StrProp(thinkingPropDesc),
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
	opts, err := agent.NewStartOptions(name, a.Model, a.Thinking)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.Start(ctx, ref, name, cwd, title, opts)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	out := paneStartJSON{OK: res.OK, State: string(res.State), Agent: res.Agent, Cwd: res.Cwd, Title: res.Title}
	if res.StartCommand != "" {
		out.Model, out.Thinking, out.StartCommand = opts.Model, opts.Thinking, res.StartCommand
		if res.OK {
			saved := e.saveNote(ctx, ref.ID, startNote(nil, opts))
			out.NoteSaved = &saved
		}
	}
	return toolkit.MarshalResult(out)
}

// startNote は note に、起動時に指定した model・thinking（指定があった項目だけ）を重ねたメモの項目を返す。
// 重なる項目は起動時の指定が勝つ（実際に使った値を書く）。note も指定も無ければ nil。note は書き換えない。
func startNote(note map[string]json.RawMessage, opts agent.StartOptions) map[string]json.RawMessage {
	if opts.IsZero() {
		return note
	}
	out := make(map[string]json.RawMessage, len(note)+2)
	for k, v := range note {
		out[k] = v
	}
	for k, v := range map[string]string{"model": opts.Model, "thinking": opts.Thinking} {
		if v != "" {
			b, _ := json.Marshal(v) // string は失敗しない
			out[k] = b
		}
	}
	return out
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
