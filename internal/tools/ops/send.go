package ops

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type sendTextArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
	Text    *string `json:"text"`
	File    *string `json:"file"`
}

type sendTextJSON struct {
	OK       bool   `json:"ok"`
	State    string `json:"state"`
	Accepted bool   `json:"accepted"`
	Chars    int    `json:"chars"`
}

func (e *env) sendTextTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "send_text",
		Description: descSendText,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane"}, toolkit.Props{
			"pane":    toolkit.PaneProp(),
			"session": toolkit.SessionProp(),
			"text":    toolkit.StrProp("貼り付ける本文（8000 字まで。制御文字は改行とタブだけ）。file と同時に指定しない"),
			"file":    toolkit.StrProp("サーバー上のファイルの絶対パス（許可された根の下、UTF-8、8000 字まで）。text と同時に指定しない"),
		}),
		Handler: e.sendText,
	}
}

func (e *env) sendText(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a sendTextArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	if (a.Text == nil) == (a.File == nil) {
		return toolkit.Result{}, toolkit.InvalidArg("text")
	}
	var text string
	if a.Text != nil {
		if text = *a.Text; text == "" {
			return toolkit.Result{}, toolkit.InvalidArg("text")
		}
		if err := e.Safety.CheckText(text); err != nil {
			return toolkit.Result{}, toolkit.WithField(err, "text")
		}
	} else {
		body, err := e.Safety.ReadFile(*a.File)
		if err != nil {
			return toolkit.Result{}, apperr.As(err)
		}
		if text = body; text == "" {
			return toolkit.Result{}, apperr.New(apperr.FileUnreadable, "field", "file")
		}
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	res, err := e.Ops.SendText(ctx, ref, text)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	return toolkit.MarshalResult(sendTextJSON{OK: res.OK, State: string(res.State), Accepted: res.Accepted, Chars: res.Chars})
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
