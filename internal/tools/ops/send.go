package ops

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type sendTextArgs struct {
	Session    *string  `json:"session"`
	Pane       *string  `json:"pane"`
	Text       *string  `json:"text"`
	File       *string  `json:"file"`
	Task       *string  `json:"task"`
	Profile    *string  `json:"profile"`
	ExtraRules []string `json:"extra_rules"`
}

// sendTextJSON のキー順は ok・state・accepted・chars（既存）のあとに sent・reaction・reaction_basis。
// sent は task で組み立てたときだけ、reaction・reaction_basis は working・stalled のペインへ送ったときだけ付く（Issue #120）。
type sendTextJSON struct {
	OK            bool   `json:"ok"`
	State         string `json:"state"`
	Accepted      bool   `json:"accepted"`
	Chars         int    `json:"chars"`
	Sent          string `json:"sent,omitempty"`
	Reaction      string `json:"reaction,omitempty"`
	ReactionBasis string `json:"reaction_basis,omitempty"`
}

func (e *env) sendTextTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "send_text",
		Description: descSendText,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane"}, toolkit.Props{
			"pane":        toolkit.PaneProp(),
			"session":     toolkit.SessionProp(),
			"text":        toolkit.StrProp("貼り付ける本文（8000 字まで。制御文字は改行とタブだけ）。file・task と同時に指定しない"),
			"file":        toolkit.StrProp("サーバー上のファイルの絶対パス（許可された根の下、UTF-8、8000 字まで）。text・task と同時に指定しない"),
			"task":        toolkit.StrProp("作業中に足す追加の指示の本文。dispatch と同じ形（追加の指示の見出し＋条件）に組み立てて送る。text・file と同時に指定しない。profile が必須"),
			"profile":     toolkit.StrProp("task のときの profiles.toml のプロファイル名"),
			"extra_rules": toolkit.StrArrayProp("task のときの追加の条件（各 1 行）"),
		}),
		Handler: e.sendText,
	}
}

func (e *env) sendText(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a sendTextArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	if a.Task == nil && (a.Profile != nil || a.ExtraRules != nil) {
		return toolkit.Result{}, toolkit.InvalidArg("task")
	}
	given := 0
	for _, set := range []bool{a.Text != nil, a.File != nil, a.Task != nil} {
		if set {
			given++
		}
	}
	if given != 1 {
		return toolkit.Result{}, toolkit.InvalidArg("text")
	}
	var text string
	var in render.DispatchInput
	switch {
	case a.Text != nil:
		if text = *a.Text; text == "" {
			return toolkit.Result{}, toolkit.InvalidArg("text")
		}
		if err := e.Safety.CheckText(text); err != nil {
			return toolkit.Result{}, toolkit.WithField(err, "text")
		}
	case a.File != nil:
		body, err := e.Safety.ReadFile(*a.File)
		if err != nil {
			return toolkit.Result{}, apperr.As(err)
		}
		if text = body; text == "" {
			return toolkit.Result{}, apperr.New(apperr.FileUnreadable, "field", "file")
		}
	default:
		var err error
		if in, err = e.checkFollowUpArgs(&a); err != nil {
			return toolkit.Result{}, err
		}
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	out := sendTextJSON{}
	if a.Task != nil {
		in.Pos = ref.Pos
		if text, err = render.Dispatch(in); err != nil {
			return toolkit.Result{}, apperr.As(err)
		}
		if err := e.Safety.CheckText(text); err != nil {
			return toolkit.Result{}, apperr.As(err)
		}
		out.Sent = text
	}
	res, err := e.Ops.SendText(ctx, ref, text)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	out.OK, out.State, out.Accepted, out.Chars = res.OK, string(res.State), res.Accepted, res.Chars
	out.Reaction, out.ReactionBasis = string(res.Reaction), res.ReactionBasis
	return toolkit.MarshalResult(out)
}

// checkFollowUpArgs は task で組み立てるときの引数（task・profile・extra_rules）を、ペインを読む前に検証し、組み立ての入力を返す。
func (e *env) checkFollowUpArgs(a *sendTextArgs) (render.DispatchInput, error) {
	in := render.DispatchInput{Profiles: *e.Profiles, ReportDir: e.Cfg.ReportDir, ExtraRules: a.ExtraRules, FollowUp: true}
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
	for _, r := range a.ExtraRules {
		if err := e.Safety.CheckLine(r, config.MaxTextRunes); err != nil {
			return in, toolkit.WithField(err, "extra_rules")
		}
	}
	return in, nil
}

type sendEscapeArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
	Confirm *bool   `json:"confirm"`
}

type sendEscapeJSON struct {
	OK          bool   `json:"ok"`
	State       string `json:"state"`
	Interrupted bool   `json:"interrupted"`
}

func (e *env) sendEscapeTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "send_escape",
		Description: descSendEscape,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane", "confirm"}, toolkit.Props{
			"pane":    toolkit.PaneProp(),
			"session": toolkit.SessionProp(),
			"confirm": toolkit.BoolProp("true でなければ実行しない"),
		}),
		Handler: e.sendEscape,
	}
}

func (e *env) sendEscape(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a sendEscapeArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	if _, err := toolkit.RequireStr(a.Pane, "pane"); err != nil {
		return toolkit.Result{}, err
	}
	if a.Confirm == nil || !*a.Confirm {
		return toolkit.Result{}, apperr.New(apperr.ConfirmRequired)
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.Escape(ctx, ref)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	return toolkit.MarshalResult(sendEscapeJSON{OK: res.OK, State: string(res.State), Interrupted: res.Interrupted})
}
