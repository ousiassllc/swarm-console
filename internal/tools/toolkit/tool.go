// Package toolkit は MCP ツールのハンドラ（view・ops）が共有する部品を持つ。
// ツールの型（Tool・Result）、依存（Deps・Env）、引数の検証、JSON Schema の部品、エラー本文、監査用の値。
// MCP の型には依存しない（コンポーネント設計 §5.3）。
package toolkit

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

// Allowed は Scope のツールを窓口に出すか。/report は ScopeReport だけ、/ops は全部。未知の窓口は出さない。
func Allowed(s Scope, endpoint string) bool {
	switch endpoint {
	case EndpointReport:
		return s == ScopeReport
	case EndpointOps:
		return true
	}
	return false
}
