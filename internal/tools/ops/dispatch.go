package ops

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type dispatchArgs struct {
	Session    *string                    `json:"session"`
	Pane       *string                    `json:"pane"`
	Agent      *string                    `json:"agent"`
	Cwd        *string                    `json:"cwd"`
	Title      *string                    `json:"title"`
	Command    *string                    `json:"command"`
	Task       *string                    `json:"task"`
	Profile    *string                    `json:"profile"`
	ExtraRules []string                   `json:"extra_rules"`
	Note       map[string]json.RawMessage `json:"note"`
}

type dispatchJSON struct {
	OK        bool   `json:"ok"`
	State     string `json:"state"`
	Accepted  bool   `json:"accepted"`
	Started   bool   `json:"started"`
	Sent      string `json:"sent"`
	NoteSaved bool   `json:"note_saved"`
}

func (e *env) dispatchTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "dispatch",
		Description: descDispatch,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane", "agent", "task", "profile"}, toolkit.Props{
			"pane":        toolkit.StrProp("shell または idle のペイン。" + toolkit.PanePropDesc),
			"session":     toolkit.SessionProp(),
			"agent":       toolkit.EnumProp("エージェント", agent.Builtin()...),
			"cwd":         toolkit.StrProp("作業フォルダの絶対パス（shell のとき必須。idle では無視）"),
			"title":       toolkit.StrProp("タイトル（shell のとき必須。80 字まで、制御文字なし）"),
			"command":     toolkit.StrProp("1 行目のスラッシュコマンド（改行・制御文字不可）"),
			"task":        toolkit.StrProp("やることの本文"),
			"profile":     toolkit.StrProp("profiles.toml のプロファイル名"),
			"extra_rules": toolkit.StrArrayProp("追加の条件（各 1 行）"),
			"note":        toolkit.ObjectProp("一緒に保存するメモ（note_set の set と同じ項目）"),
		}),
		Handler: e.dispatch,
	}
}

// dispatch は API 仕様 §5.6。何かを送る前に引数・指示文・メモ・cwd をすべて検証し、
// 1 つでも落ちたら何も送らず何も保存しない。メモは送信後（受理の成否によらず）に保存する（P-23）。
func (e *env) dispatch(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a dispatchArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	in, err := e.checkDispatchArgs(&a)
	if err != nil {
		return toolkit.Result{}, err
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	in.Pos = ref.Pos
	text, err := render.Dispatch(in)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	if err := e.Safety.CheckText(text); err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	args := pane.DispatchArgs{Agent: *a.Agent, Text: text}
	if args.Cwd, args.Title, err = e.dispatchTarget(ctx, ref, &a); err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.Dispatch(ctx, ref, args)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	out := dispatchJSON{OK: res.OK, State: string(res.State), Accepted: res.Accepted, Started: res.Started}
	// 送信が行われたのは、指示文を実際に貼ったとき（Ops が事実で埋める）。
	// shell で起動に失敗したときは何も送っていない（指示文もメモも残さない）。
	if res.Sent {
		out.Sent, out.NoteSaved = text, e.saveNote(ctx, ref.ID, a.Note)
	}
	return toolkit.MarshalResult(out)
}

// checkDispatchArgs は、ペインを読む前にできる引数の検証（型・必須・プロファイル・1 行の項目・メモ）を行い、
// render.Dispatch の入力を返す。
func (e *env) checkDispatchArgs(a *dispatchArgs) (render.DispatchInput, error) {
	in := render.DispatchInput{
		Profiles: *e.Profiles, ReportDir: e.Cfg.ReportDir, ExtraRules: a.ExtraRules,
	}
	if _, err := toolkit.RequireStr(a.Pane, "pane"); err != nil {
		return in, err
	}
	if _, err := checkAgent(a.Agent); err != nil {
		return in, err
	}
	var err error
	if in.Task, err = toolkit.RequireStr(a.Task, "task"); err != nil {
		return in, err
	}
	if in.Profile, err = toolkit.RequireStr(a.Profile, "profile"); err != nil {
		return in, err
	}
	if _, ok := e.Profiles.Profiles[in.Profile]; !ok {
		return in, toolkit.InvalidArg("profile")
	}
	if a.Command != nil {
		if err := e.Safety.CheckLine(*a.Command, config.MaxTextRunes); err != nil {
			return in, toolkit.WithField(err, "command")
		}
		in.Command = *a.Command
	}
	for _, r := range a.ExtraRules {
		if err := e.Safety.CheckLine(r, config.MaxTextRunes); err != nil {
			return in, toolkit.WithField(err, "extra_rules")
		}
	}
	if a.Note != nil {
		if err := notes.Check(a.Note, e.Detect); err != nil {
			return in, apperr.As(err)
		}
	}
	return in, nil
}

// dispatchTarget はペインの状態が shell のときだけ必要な cwd・title を検証して返す。
// idle のペインでは無視する。ほかの状態は Ops.Dispatch が invalid_state にする。
func (e *env) dispatchTarget(ctx context.Context, ref pane.Ref, a *dispatchArgs) (cwd, title string, err error) {
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return "", "", apperr.Wrap(apperr.Internal, err)
	}
	p, ok := snap.Find(ref.ID)
	if !ok || p.State != agent.StateShell {
		return "", "", nil
	}
	return e.checkStartTarget(a.Cwd, a.Title)
}

// saveNote は送信後にメモを保存する。失敗しても送信は済んでいるのでエラーにせず false を返す。
func (e *env) saveNote(ctx context.Context, id string, note map[string]json.RawMessage) bool {
	if note == nil {
		return false
	}
	if _, err := e.Notes.Set(ctx, id, note); err != nil {
		e.Logger.Warn("dispatch 後のメモの保存に失敗しました", "pane", id, "code", toolkit.ErrorCode(err))
		return false
	}
	return true
}
