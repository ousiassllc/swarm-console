package tools

import (
	"context"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/session"
)

type endpointKey struct{}

// WithEndpoint は呼び出しの窓口（EndpointReport・EndpointOps）を ctx に載せる。mcpapi が tools/call の前に付ける。
func WithEndpoint(ctx context.Context, endpoint string) context.Context {
	return context.WithValue(ctx, endpointKey{}, endpoint)
}

func endpointOf(ctx context.Context) string {
	s, _ := ctx.Value(endpointKey{}).(string)
	return s
}

const sessionPropDesc = "対象の tmux セッション名。省略は、許可されたセッションがちょうど 1 つのときだけ（それを使う）"

func sessionProp() map[string]any { return strProp(sessionPropDesc) }

// allowed は c が触れるセッション（トークンの allowed_sessions と config の sessions の共通部分）。
func (e *env) allowed(c auth.Client) []string {
	return session.Allowed(c.AllowedSessions, e.Cfg.Sessions)
}

// scoped は session 引数から対象セッションを決め、そのセッションだけのスナップショットを返す（ForSession）。
// 省略時の既定・許可外・config に無い名前の扱いは session.Pick。
func (e *env) scoped(ctx context.Context, c auth.Client, arg *string) (*pane.Snapshot, error) {
	s, err := session.Pick(e.allowed(c), e.Cfg.Sessions, arg)
	if err != nil {
		return nil, err
	}
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	return snap.ForSession(s), nil
}

// resolvePane は pane 引数を、session 引数（または既定）のセッションの中で解決する。
func (e *env) resolvePane(ctx context.Context, c auth.Client, sessionArg, paneArg *string) (pane.Ref, error) {
	name, err := requireStr(paneArg, "pane")
	if err != nil {
		return pane.Ref{}, err
	}
	allowed := e.allowed(c)
	s, err := session.Pick(allowed, e.Cfg.Sessions, sessionArg)
	if err != nil {
		return pane.Ref{}, err
	}
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return pane.Ref{}, apperr.Wrap(apperr.Internal, err)
	}
	return session.Resolve(snap, allowed, s, name)
}
