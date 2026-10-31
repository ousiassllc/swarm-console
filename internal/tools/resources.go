package tools

import (
	"context"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/render"
)

// PaneRef は resources/list が列挙するペイン 1 つ。
type PaneRef struct {
	Pos string
	ID  string
}

// Panes は現在のスナップショットのペインを位置順で返す（resources/list 用）。
func (r *Registry) Panes(ctx context.Context) ([]PaneRef, error) {
	snap, err := r.env.Registry.Current(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	out := make([]PaneRef, 0, len(snap.Panes))
	for i := range snap.Panes {
		out = append(out, PaneRef{Pos: snap.Panes[i].Pos, ID: snap.Panes[i].ID})
	}
	return out, nil
}

// ReportMarkdown は report ツール（markdown・既定言語）と同じ本文。paneArg が空なら全体、
// あれば 1 ペインの詳しい版（resource の swarm://report・swarm://pane/<位置>）。
// 解決できなければ apperr.PaneNotFound。
func (r *Registry) ReportMarkdown(ctx context.Context, paneArg string) (string, error) {
	var p *string
	if paneArg != "" {
		p = &paneArg
	}
	text, err := r.env.renderReport(ctx, render.ReportInput{}, p)
	if err != nil {
		return "", apperr.As(err)
	}
	return text, nil
}

// NoteJSON は note_get ツールと同じ JSON（resource の swarm://notes/<位置>）。
func (r *Registry) NoteJSON(ctx context.Context, paneArg string) (string, error) {
	if paneArg == "" {
		return "", apperr.New(apperr.PaneNotFound)
	}
	ref, err := r.env.Registry.Resolve(ctx, paneArg)
	if err != nil {
		return "", apperr.As(err)
	}
	res, err := marshalResult(r.env.noteOf(ref.ID, ref.Pos))
	return res.Text, err
}
