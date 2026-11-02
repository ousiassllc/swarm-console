package tools

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/notes"
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

func (e *env) noteSetTool() Tool {
	return Tool{
		Name:        "note_set",
		Description: descNoteSet,
		Scope:       ScopeOps,
		Schema: object([]string{"pane", "set"}, props{
			"pane":    paneProp(),
			"session": sessionProp(),
			"set":     objectProp("書き換える項目だけ。値が null ならその項目を消す（updated_at・log は不可）"),
		}),
		Handler: e.noteSet,
	}
}

func (e *env) noteSet(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a noteSetArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	if a.Set == nil {
		return Result{}, invalidArg("set")
	}
	ref, err := e.resolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return Result{}, err
	}
	n, err := e.Notes.Set(ctx, ref.ID, a.Set)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return marshalResult(noteSetJSON{OK: true, Note: n})
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

func (e *env) noteLogTool() Tool {
	return Tool{
		Name:        "note_log",
		Description: descNoteLog,
		Scope:       ScopeOps,
		Schema: object([]string{"pane", "text"}, props{
			"pane":    paneProp(),
			"session": sessionProp(),
			"text":    strProp("ログの 1 行（500 字まで、改行なし）"),
		}),
		Handler: e.noteLog,
	}
}

func (e *env) noteLog(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a noteLogArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	text, err := requireStr(a.Text, "text")
	if err != nil {
		return Result{}, err
	}
	ref, err := e.resolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return Result{}, err
	}
	size, err := e.Notes.Log(ctx, ref.ID, text)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	return marshalResult(noteLogJSON{OK: true, LogSize: size})
}
