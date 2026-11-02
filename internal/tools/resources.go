package tools

import (
	"context"
	"slices"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/session"
)

// PaneRef は resources/list が列挙するペイン 1 つ。Pos は resource の URI に使う識別子。
type PaneRef struct {
	Pos string
	ID  string
}

// resourceScope は resource を呼び出し元のセッションにどう結び付けるか。
// 許可がちょうど 1 つなら、そのセッションの中の位置（T1）で指す。2 つ以上なら、セッションをまたいで一意な
// 接頭辞つきの位置（1:T1）か tmux ID で指す（swarm://report は session を決められないので使えない）。
func (e *env) resourceScope(c auth.Client) (allowed []string, in string) {
	allowed = e.allowed(c)
	if len(allowed) == 1 {
		in = allowed[0]
	}
	return allowed, in
}

// Panes は現在のスナップショットのうち、c が許可されたセッションのペインを位置順で返す（resources/list 用）。
func (r *Registry) Panes(ctx context.Context, c auth.Client) ([]PaneRef, error) {
	snap, err := r.env.Registry.Current(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	allowed, in := r.env.resourceScope(c)
	out := make([]PaneRef, 0, len(snap.Panes))
	for i := range snap.Panes {
		p := &snap.Panes[i]
		if !slices.Contains(allowed, p.Session) {
			continue
		}
		pos := p.ReportPos()
		if in != "" {
			pos = p.LocalPos()
		}
		out = append(out, PaneRef{Pos: pos, ID: p.ID})
	}
	return out, nil
}

// ReportReadable は swarm://report を c が読めるか（許可がちょうど 1 セッション）。
func (r *Registry) ReportReadable(c auth.Client) bool {
	_, in := r.env.resourceScope(c)
	return in != ""
}

// ReportMarkdown は report ツール（markdown・既定言語）と同じ本文。paneArg が空なら全体
// （許可がちょうど 1 セッションのときだけ。それ以外は session_required）、あれば 1 ペインの詳しい版
// （resource の swarm://report・swarm://pane/<位置>）。許可外のペインは session_forbidden、
// 解決できなければ apperr.PaneNotFound。
func (r *Registry) ReportMarkdown(ctx context.Context, c auth.Client, paneArg string) (string, error) {
	e := r.env
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return "", apperr.Wrap(apperr.Internal, err)
	}
	allowed, in := e.resourceScope(c)
	if paneArg == "" {
		s, err := session.Pick(allowed, e.Cfg.Sessions, nil)
		if err != nil {
			return "", err
		}
		return e.renderResource(ctx, snap.ForSession(s), nil)
	}
	ref, err := session.Resolve(snap, allowed, in, paneArg)
	if err != nil {
		return "", err
	}
	p, _ := snap.Find(ref.ID)
	return e.renderResource(ctx, snap.ForSession(p.Session), &ref)
}

func (e *env) renderResource(ctx context.Context, snap *pane.Snapshot, ref *pane.Ref) (string, error) {
	text, err := e.renderReport(ctx, render.ReportInput{}, snap, ref)
	if err != nil {
		return "", apperr.As(err)
	}
	return text, nil
}

// NoteJSON は note_get ツールと同じ JSON（resource の swarm://notes/<位置>）。
// 位置は Panes と同じ識別子（許可が 1 セッションならセッション内の位置、2 つ以上なら接頭辞つき）。
func (r *Registry) NoteJSON(ctx context.Context, c auth.Client, paneArg string) (string, error) {
	if paneArg == "" {
		return "", apperr.New(apperr.PaneNotFound)
	}
	snap, err := r.env.Registry.Current(ctx)
	if err != nil {
		return "", apperr.Wrap(apperr.Internal, err)
	}
	allowed, in := r.env.resourceScope(c)
	ref, err := session.Resolve(snap, allowed, in, paneArg)
	if err != nil {
		return "", err
	}
	pos := ref.Pos
	if in != "" {
		pos = ref.Local
	}
	res, err := marshalResult(r.env.noteOf(ref.ID, pos))
	return res.Text, err
}
