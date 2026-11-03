// Package tools は MCP ツール 19 個のハンドラ（引数検証・pane の解決・エラーコードへの変換）を持つ。
// MCP の型には依存せず、mcpapi が Tool を SDK のサーバーへ登録する（コンポーネント設計 §5.3）。
package tools

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

// 窓口の名前（API 仕様 §1）。
const (
	EndpointReport = "/report"
	EndpointOps    = "/ops"
)

// Scope はツールを出す窓口の範囲。
type Scope int

const (
	// ScopeReport は /report と /ops の両方に出す（見るだけ）。
	ScopeReport Scope = iota
	// ScopeOps は /ops だけに出す（操作）。
	ScopeOps
)

// Result はツールの成功結果。Text は content[0].text にそのまま入る
// （report の markdown 形式は markdown 本文、それ以外は JSON 1 つ）。
type Result struct {
	Text string
}

// Handler はツール 1 つの実装。失敗は *apperr.Error で返す（mcpapi が isError に変換する）。
type Handler func(ctx context.Context, c auth.Client, args json.RawMessage) (Result, error)

// Tool は 1 ツールの定義。Description はクライアントによらず同じ固定文。
type Tool struct {
	Name        string
	Description string
	Scope       Scope
	Schema      any // 引数の JSON Schema（additionalProperties:false）
	Handler     Handler
}

// Registry は組み立て済みのツール一覧。作ったあと変更しない。
type Registry struct {
	all    []Tool
	byName map[string]int
	env    *env
}

func newRegistry(e *env) *Registry {
	all := e.all()
	r := &Registry{all: all, byName: make(map[string]int, len(all)), env: e}
	for i := range all {
		r.byName[all[i].Name] = i
	}
	return r
}

// ForEndpoint は窓口に出すツールを登録順で返す。/report は ScopeReport だけ、/ops は全部。
// 未知の窓口は nil。
func (r *Registry) ForEndpoint(endpoint string) []Tool {
	var out []Tool
	for i := range r.all {
		if allowed(r.all[i].Scope, endpoint) {
			out = append(out, r.all[i])
		}
	}
	return out
}

// Lookup は窓口に出ているツールを名前で探す。窓口に出ていなければ false。
func (r *Registry) Lookup(endpoint, name string) (Tool, bool) {
	i, ok := r.byName[name]
	if !ok || !allowed(r.all[i].Scope, endpoint) {
		return Tool{}, false
	}
	return r.all[i], true
}

func allowed(s Scope, endpoint string) bool {
	switch endpoint {
	case EndpointReport:
		return s == ScopeReport
	case EndpointOps:
		return true
	}
	return false
}
