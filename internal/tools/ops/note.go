package ops

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type noteSetArgs struct {
	Session *string                    `json:"session"`
	Pane    *string                    `json:"pane"`
	Set     map[string]json.RawMessage `json:"set"`
}

type noteSetJSON struct {
	OK   bool        `json:"ok"`
	Note *notes.Note `json:"note"`
}

func (e *env) noteSetTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "note_set",
		Description: descNoteSet,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane", "set"}, toolkit.Props{
			"pane":    toolkit.PaneProp(),
			"session": toolkit.SessionProp(),
			"set":     toolkit.ObjectProp("書き換える項目だけ。値が null ならその項目を消す（updated_at・log は不可）"),
		}),
		Handler: e.noteSet,
	}
}

func (e *env) noteSet(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a noteSetArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	if a.Set == nil {
		return toolkit.Result{}, toolkit.InvalidArg("set")
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	n, err := e.Notes.Set(ctx, ref.ID, a.Set)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	return toolkit.MarshalResult(noteSetJSON{OK: true, Note: n})
}

type noteLogArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
	Text    *string `json:"text"`
}

type noteLogJSON struct {
	OK      bool `json:"ok"`
	LogSize int  `json:"log_size"`
}

func (e *env) noteLogTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "note_log",
		Description: descNoteLog,
		Scope:       toolkit.ScopeOps,
		Schema: toolkit.Object([]string{"pane", "text"}, toolkit.Props{
			"pane":    toolkit.PaneProp(),
			"session": toolkit.SessionProp(),
			"text":    toolkit.StrProp("ログの 1 行（500 字まで、改行なし）"),
		}),
		Handler: e.noteLog,
	}
}

func (e *env) noteLog(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a noteLogArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	text, err := toolkit.RequireStr(a.Text, "text")
	if err != nil {
		return toolkit.Result{}, err
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	size, err := e.Notes.Log(ctx, ref.ID, text)
	if err != nil {
		return toolkit.Result{}, apperr.As(err)
	}
	return toolkit.MarshalResult(noteLogJSON{OK: true, LogSize: size})
}
