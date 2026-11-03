package tools

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/notes"
)

// note_history の limit（API 仕様 §5.4）。
const (
	historyDefaultLimit = 20
	historyMaxLimit     = 100
)

type noteGetJSON struct {
	Pane string      `json:"pane"`
	ID   string      `json:"id"`
	Note *notes.Note `json:"note"`
}

func (e *env) noteGetTool() Tool {
	return Tool{
		Name:        "note_get",
		Description: descNoteGet,
		Scope:       ScopeReport,
		Schema:      object([]string{"pane"}, props{"pane": paneProp(), "session": sessionProp()}),
		Handler:     e.noteGet,
	}
}

func (e *env) noteGet(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a paneArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	ref, err := e.resolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return Result{}, err
	}
	return marshalResult(e.noteOf(ref.ID, ref.Local))
}

// noteOf は note_get の本体（resource の swarm://notes/<位置> からも使う）。
func (e *env) noteOf(id, pos string) noteGetJSON {
	out := noteGetJSON{Pane: pos, ID: id}
	if n, ok := e.Notes.Get(id); ok {
		out.Note = n
	}
	return out
}

type noteHistoryArgs struct {
	Repo      *string  `json:"repo"`
	From      *string  `json:"from"`
	Origin    *string  `json:"origin"`
	StartedBy *string  `json:"started_by"`
	Items     []string `json:"items"`
	Limit     *int     `json:"limit"`
}

type noteHistoryJSON struct {
	Entries []notes.HistoryEntry `json:"entries"`
	Total   int                  `json:"total"`
}

func (e *env) noteHistoryTool() Tool {
	return Tool{
		Name:        "note_history",
		Description: descNoteHistory,
		Scope:       ScopeReport,
		Schema: object(nil, props{
			"repo":       strProp("リポジトリ（完全一致）"),
			"from":       strProp("依頼元（完全一致）"),
			"origin":     strProp("始まったきっかけ（user か agent。完全一致）"),
			"started_by": strProp("始めた人やエージェントの名前（完全一致）"),
			"items":      strArrayProp("Issue・PR。指定したものをすべて含む履歴だけ"),
			"limit":      intProp("件数（既定 20）", 1, historyMaxLimit),
		}),
		Handler: e.noteHistory,
	}
}

func (e *env) noteHistory(_ context.Context, _ auth.Client, raw json.RawMessage) (Result, error) {
	var a noteHistoryArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	limit := historyDefaultLimit
	if a.Limit != nil {
		limit = *a.Limit
		if limit < 1 || limit > historyMaxLimit {
			return Result{}, invalidArg("limit")
		}
	}
	entries, total := e.Notes.History(notes.HistoryFilter{
		Repo: deref(a.Repo), From: deref(a.From), Origin: deref(a.Origin), StartedBy: deref(a.StartedBy), Items: a.Items, Limit: limit,
	})
	if entries == nil {
		entries = []notes.HistoryEntry{}
	}
	return marshalResult(noteHistoryJSON{Entries: entries, Total: total})
}
