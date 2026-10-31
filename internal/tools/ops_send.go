package tools

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

type sendTextArgs struct {
	Pane *string `json:"pane"`
	Text *string `json:"text"`
	File *string `json:"file"`
}

type sendTextJSON struct {
	OK       bool   `json:"ok"`
	State    string `json:"state"`
	Accepted bool   `json:"accepted"`
	Chars    int    `json:"chars"`
}

func (e *env) sendTextTool() Tool {
	return Tool{
		Name:        "send_text",
		Description: descSendText,
		Scope:       ScopeOps,
		Schema: object([]string{"pane"}, props{
			"pane": paneProp(),
			"text": strProp("貼り付ける本文（8000 字まで。制御文字は改行とタブだけ）。file と同時に指定しない"),
			"file": strProp("サーバー上のファイルの絶対パス（許可された根の下、UTF-8、8000 字まで）。text と同時に指定しない"),
		}),
		Handler: e.sendText,
	}
}

func (e *env) sendText(ctx context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a sendTextArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	if (a.Text == nil) == (a.File == nil) {
		return Result{}, invalidArg("text")
	}
	var text string
	if a.Text != nil {
		if text = *a.Text; text == "" {
			return Result{}, invalidArg("text")
		}
		if err := e.Safety.CheckText(text); err != nil {
			return Result{}, withField(err, "text")
		}
	} else {
		body, err := e.Safety.ReadFile(*a.File)
		if err != nil {
			return Result{}, apperr.As(err)
		}
		if text = body; text == "" {
			return Result{}, apperr.New(apperr.FileUnreadable, "field", "file")
		}
	}
	ref, err := resolvePane(ctx, e.Registry, a.Pane)
	if err != nil {
		return Result{}, err
	}
	res, err := e.Ops.SendText(ctx, ref, text)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return marshalResult(sendTextJSON{OK: res.OK, State: string(res.State), Accepted: res.Accepted, Chars: res.Chars})
}

type sendEscapeArgs struct {
	Pane    *string `json:"pane"`
	Confirm *bool   `json:"confirm"`
}

type sendEscapeJSON struct {
	OK          bool   `json:"ok"`
	State       string `json:"state"`
	Interrupted bool   `json:"interrupted"`
}

func (e *env) sendEscapeTool() Tool {
	return Tool{
		Name:        "send_escape",
		Description: descSendEscape,
		Scope:       ScopeOps,
		Schema: object([]string{"pane", "confirm"}, props{
			"pane":    paneProp(),
			"confirm": boolProp("true でなければ実行しない"),
		}),
		Handler: e.sendEscape,
	}
}

func (e *env) sendEscape(ctx context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a sendEscapeArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	if _, err := requireStr(a.Pane, "pane"); err != nil {
		return Result{}, err
	}
	if a.Confirm == nil || !*a.Confirm {
		return Result{}, apperr.New(apperr.ConfirmRequired)
	}
	ref, err := resolvePane(ctx, e.Registry, a.Pane)
	if err != nil {
		return Result{}, err
	}
	res, err := e.Ops.Escape(ctx, ref)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return marshalResult(sendEscapeJSON{OK: res.OK, State: string(res.State), Interrupted: res.Interrupted})
}
