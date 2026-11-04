package toolkit

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

// EndpointOf は ctx の窓口（無ければ ""）。
func EndpointOf(ctx context.Context) string {
	s, _ := ctx.Value(endpointKey{}).(string)
	return s
}

const sessionPropDesc = "対象の tmux セッション名。省略は、許可されたセッションがちょうど 1 つのときだけ（それを使う）"

// SessionProp は session 引数。
func SessionProp() map[string]any { return StrProp(sessionPropDesc) }

// Allowed は c が触れるセッション（トークンの allowed_sessions と config の sessions の共通部分）。
// 認証を通っていない（名前の無い）Client は何も許可しない。AllowedSessions が nil（全許可）の番兵に当たらないようにする。
func (e *Env) Allowed(c auth.Client) []string {
	if c.Name == "" {
		return []string{}
	}
	return session.Allowed(c.AllowedSessions, e.Cfg.Sessions)
}

// Scoped は session 引数から対象セッションを決め、そのセッションだけのスナップショットを返す（ForSession）。
// 省略時の既定・許可外・config に無い名前の扱いは session.Pick。
func (e *Env) Scoped(ctx context.Context, c auth.Client, arg *string) (*pane.Snapshot, error) {
	s, err := session.Pick(e.Allowed(c), e.Cfg.Sessions, arg)
	if err != nil {
		return nil, err
	}
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	return snap.ForSession(s), nil
}

// ResolvePane は pane 引数を、session 引数（または既定）のセッションの中で解決する。
func (e *Env) ResolvePane(ctx context.Context, c auth.Client, sessionArg, paneArg *string) (pane.Ref, error) {
	name, err := RequireStr(paneArg, "pane")
	if err != nil {
		return pane.Ref{}, err
	}
	allowed := e.Allowed(c)
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
