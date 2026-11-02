// Package session は呼び出し元ごとの許可セッション（tokens.toml の allowed_sessions）の検査と、
// セッション文脈でのペイン解決を持つ。ツール層・resources が使う純粋な関数だけで、状態を持たない。
package session

import (
	"slices"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// argEchoMax は pane_not_found の details に載せる引数の最大字数。
const argEchoMax = 100

// Allowed は呼び出し元が触れるセッション。granted（auth.Client.AllowedSessions。nil は全部）と
// config の sessions の共通部分を config の順で返す。
func Allowed(granted, configured []string) []string {
	out := make([]string, 0, len(configured))
	for _, s := range configured {
		if granted == nil || slices.Contains(granted, s) {
			out = append(out, s)
		}
	}
	return out
}

// Pick はツールの session 引数から対象のセッションを決める。
//   - 引数あり: config の sessions に無ければ invalid_args（field=session）、許可されていなければ session_forbidden。
//   - 引数なし: 許可がちょうど 1 つならそれ、それ以外は session_required（許可の一覧は返さず、個数だけ）。
func Pick(allowed, configured []string, arg *string) (string, error) {
	if arg == nil {
		if len(allowed) == 1 {
			return allowed[0], nil
		}
		return "", apperr.New(apperr.SessionRequired, "allowed_count", len(allowed))
	}
	switch {
	case !slices.Contains(configured, *arg):
		return "", apperr.New(apperr.InvalidArgs, "field", "session")
	case !slices.Contains(allowed, *arg):
		return "", apperr.New(apperr.SessionForbidden, "session", *arg)
	}
	return *arg, nil
}

// Resolve は pane 引数を snap のペインに解決する。in が空でなければそのセッションの中だけを探し、
// 空なら許可されたセッション全体から探す（resources の識別子。位置は接頭辞つきの名前か tmux ID だけ）。
//
// 解釈できる形は、tmux ID（%12）、セッション内の位置（T1。in があるときだけ。大文字小文字不問）、
// 接頭辞つきの位置（1:T1・pane_names の固定名）、<セッション>:<セッション内の位置>。
// 見つからないとき、別のセッションのペイン（tmux ID・全体の名前で当たるもの）を指していれば、そのセッションが許可外なら
// session_forbidden、許可内なら invalid_args（field=pane。session と pane が食い違う）。許可外のペインの有無は、
// この 2 つと pane_not_found の違いで分かる（P-79）。どれにも当たらなければ pane_not_found。
func Resolve(snap *pane.Snapshot, allowed []string, in, arg string) (pane.Ref, error) {
	for i := range snap.Panes {
		p := &snap.Panes[i]
		if inScope(p, allowed, in) && matches(p, arg, in != "") {
			return pane.Ref{ID: p.ID, Pos: p.ReportPos(), Local: p.LocalPos()}, nil
		}
	}
	for i := range snap.Panes {
		p := &snap.Panes[i]
		// セッション文脈では、接頭辞のない名前（T1）はセッション内の位置なので、ほかのセッションの同名ペインを指さない。
		plain := in != "" && !p.Fixed && strings.EqualFold(p.ReportPos(), p.LocalPos()) && strings.EqualFold(arg, p.LocalPos())
		if plain || !matches(p, arg, false) {
			continue
		}
		if !slices.Contains(allowed, p.Session) {
			return pane.Ref{}, apperr.New(apperr.SessionForbidden, "session", p.Session)
		}
		return pane.Ref{}, apperr.New(apperr.InvalidArgs, "field", "pane")
	}
	shown, _ := pane.TruncateRunes(arg, argEchoMax)
	return pane.Ref{}, apperr.New(apperr.PaneNotFound, "pane", shown)
}

// inScope は p が解決の対象か。in があればそのセッションのペイン、無ければ許可されたセッションのペイン。
func inScope(p *pane.PaneState, allowed []string, in string) bool {
	if in != "" {
		return p.Session == in
	}
	return slices.Contains(allowed, p.Session)
}

// matches は arg が p を指すか。local が true のときだけセッション内の位置でも当てる。
func matches(p *pane.PaneState, arg string, local bool) bool {
	global := p.ReportPos()
	if p.ID == arg || strings.EqualFold(global, arg) {
		return true
	}
	if local && strings.EqualFold(p.LocalPos(), arg) {
		return true
	}
	rest, ok := strings.CutPrefix(arg, p.Session+":")
	return ok && strings.EqualFold(p.LocalPos(), rest)
}
