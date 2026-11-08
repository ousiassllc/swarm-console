// Package tools は MCP ツール 20 個のハンドラ（引数検証・pane の解決・エラーコードへの変換）を組み立てる。
// ハンドラは view（見る）と ops（操作）に、共有の部品は toolkit にある。
// MCP の型には依存せず、mcpapi が toolkit.Tool を SDK のサーバーへ登録する（コンポーネント設計 §5.3）。
package tools

import (
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

// Registry は組み立て済みのツール一覧。作ったあと変更しない。
type Registry struct {
	all    []toolkit.Tool
	byName map[string]int
	env    *toolkit.Env
}

func newRegistry(e *toolkit.Env) *Registry {
	all := builtin(e)
	r := &Registry{all: all, byName: make(map[string]int, len(all)), env: e}
	for i := range all {
		r.byName[all[i].Name] = i
	}
	return r
}

// ForEndpoint は窓口に出すツールを登録順で返す。/report は ScopeReport だけ、/ops は全部。
// 未知の窓口は nil。
func (r *Registry) ForEndpoint(endpoint string) []toolkit.Tool {
	var out []toolkit.Tool
	for i := range r.all {
		if toolkit.Allowed(r.all[i].Scope, endpoint) {
			out = append(out, r.all[i])
		}
	}
	return out
}

// Lookup は窓口に出ているツールを名前で探す。窓口に出ていなければ false。
func (r *Registry) Lookup(endpoint, name string) (toolkit.Tool, bool) {
	i, ok := r.byName[name]
	if !ok || !toolkit.Allowed(r.all[i].Scope, endpoint) {
		return toolkit.Tool{}, false
	}
	return r.all[i], true
}
