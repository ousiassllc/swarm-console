package tools

import (
	"context"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/view"
)

// Panes は現在のスナップショットのうち、c が許可されたセッションのペインを位置順で返す（resources/list 用）。
func (r *Registry) Panes(ctx context.Context, c auth.Client) ([]view.PaneRef, error) {
	return view.Panes(ctx, r.env, c)
}

// ReportReadable は swarm://report を c が読めるか（許可がちょうど 1 セッション）。
func (r *Registry) ReportReadable(c auth.Client) bool { return view.ReportReadable(r.env, c) }

// ReportMarkdown は report ツール（markdown・既定言語）と同じ本文。paneArg が空なら全体、あれば 1 ペインの詳しい版
// （resource の swarm://report・swarm://pane/<位置>）。
func (r *Registry) ReportMarkdown(ctx context.Context, c auth.Client, paneArg string) (string, error) {
	return view.ReportMarkdown(ctx, r.env, c, paneArg)
}

// NoteJSON は note_get ツールと同じ JSON（resource の swarm://notes/<位置>）。
func (r *Registry) NoteJSON(ctx context.Context, c auth.Client, paneArg string) (string, error) {
	return view.NoteJSON(ctx, r.env, c, paneArg)
}
