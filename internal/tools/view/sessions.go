package view

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type sessionsListArgs struct {
	All *bool `json:"all"`
}

type sessionLayoutJSON struct {
	Rows int   `json:"rows"`
	Cols []int `json:"cols"`
}

// sessionJSON は許可されたセッション。許可外（all=true のとき）は sessionStubJSON で、layout・panes を持たない。
type sessionJSON struct {
	ID       string            `json:"id"`
	Allowed  bool              `json:"allowed"`
	Operable bool              `json:"operable"`
	Layout   sessionLayoutJSON `json:"layout"`
	Panes    []string          `json:"panes"`
}

type sessionStubJSON struct {
	ID       string `json:"id"`
	Allowed  bool   `json:"allowed"`
	Operable bool   `json:"operable"`
}

func (e *env) sessionsListTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "sessions_list",
		Description: descSessionsList,
		Scope:       toolkit.ScopeReport,
		Schema:      toolkit.Object(nil, toolkit.Props{"all": toolkit.BoolProp("true なら許可されていないセッションも id だけで含める（layout・panes は出さない）。既定は false")}),
		Handler:     e.sessionsList,
	}
}

func (e *env) sessionsList(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a sessionsListArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return toolkit.Result{}, apperr.Wrap(apperr.Internal, err)
	}
	all := a.All != nil && *a.All
	allowed := make(map[string]bool)
	for _, s := range e.Allowed(c) {
		allowed[s] = true
	}
	// 操作できるのは、許可されたセッションを、/ops 経由で、ops スコープのトークンで呼ぶとき。
	canOperate := c.Scope == auth.ScopeOps && toolkit.EndpointOf(ctx) == toolkit.EndpointOps
	out := make([]any, 0, len(e.Cfg.Sessions))
	for _, id := range e.Cfg.Sessions {
		switch {
		case allowed[id]:
			out = append(out, newSessionJSON(id, canOperate, snap.ForSession(id)))
		case all:
			out = append(out, sessionStubJSON{ID: id})
		}
	}
	return toolkit.MarshalResult(struct {
		Sessions []any `json:"sessions"`
	}{out})
}

func newSessionJSON(id string, operable bool, s *pane.Snapshot) sessionJSON {
	panes := make([]string, 0, len(s.Panes))
	for i := range s.Panes {
		panes = append(panes, s.Panes[i].Pos)
	}
	return sessionJSON{
		ID: id, Allowed: true, Operable: operable,
		Layout: sessionLayoutJSON{Rows: s.Layout.Rows, Cols: append([]int{}, s.Layout.Cols...)},
		Panes:  panes,
	}
}
