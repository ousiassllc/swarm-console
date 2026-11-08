package tools

import (
	"github.com/ousiassllc/swarm-console/internal/tools/ops"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
	"github.com/ousiassllc/swarm-console/internal/tools/view"
)

// builtin は登録順（API 仕様 §1.1）の 20 ツール。見る 10 のあとに操作 10 個。
func builtin(e *toolkit.Env) []toolkit.Tool {
	return append(view.Tools(e), ops.Tools(e)...)
}
