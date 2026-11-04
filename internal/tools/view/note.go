package view

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
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

func (e *env) noteGetTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "note_get",
		Description: descNoteGet,
		Scope:       toolkit.ScopeReport,
		Schema:      toolkit.Object([]string{"pane"}, toolkit.Props{"pane": toolkit.PaneProp(), "session": toolkit.SessionProp()}),
		Handler:     e.noteGet,
	}
}

func (e *env) noteGet(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a toolkit.PaneArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	ref, err := e.ResolvePane(ctx, c, a.Session, a.Pane)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.MarshalResult(e.noteOf(ref.ID, ref.Local))
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

func (e *env) noteHistoryTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "note_history",
		Description: descNoteHistory,
		Scope:       toolkit.ScopeReport,
		Schema: toolkit.Object(nil, toolkit.Props{
			"repo":       toolkit.StrProp("リポジトリ（完全一致）"),
			"from":       toolkit.StrProp("依頼元（完全一致）"),
			"origin":     toolkit.StrProp("始まったきっかけ（user か agent。完全一致）"),
			"started_by": toolkit.StrProp("始めた人やエージェントの名前（完全一致）"),
			"items":      toolkit.StrArrayProp("Issue・PR。指定したものをすべて含む履歴だけ"),
			"limit":      toolkit.IntProp("件数（既定 20）", 1, historyMaxLimit),
		}),
		Handler: e.noteHistory,
	}
}

func (e *env) noteHistory(_ context.Context, _ auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a noteHistoryArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	limit := historyDefaultLimit
	if a.Limit != nil {
		limit = *a.Limit
		if limit < 1 || limit > historyMaxLimit {
			return toolkit.Result{}, toolkit.InvalidArg("limit")
		}
	}
	entries, total := e.Notes.History(notes.HistoryFilter{
		Repo: deref(a.Repo), From: deref(a.From), Origin: deref(a.Origin), StartedBy: deref(a.StartedBy), Items: a.Items, Limit: limit,
	})
	if entries == nil {
		entries = []notes.HistoryEntry{}
	}
	return toolkit.MarshalResult(noteHistoryJSON{Entries: entries, Total: total})
}
