package mcpapi

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// toolHandler は tools.Tool を SDK のハンドラにする。ツールの失敗は isError: true の結果（API 仕様 §3.1）。
// 引数の検証はハンドラ側（未知キー・型・範囲はすべて invalid_args）。panic も internal にして落とさない。
func (b *builder) toolHandler(t tools.Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, _ error) {
		defer func() {
			if r := recover(); r != nil {
				res = errorResult(apperr.New(apperr.Internal))
			}
		}()
		c, _ := auth.FromContext(ctx)
		ctx = tools.WithEndpoint(ctx, b.endpoint)
		var args json.RawMessage
		if req.Params != nil {
			args = req.Params.Arguments
		}
		out, err := t.Handler(ctx, c, args)
		if err != nil {
			return errorResult(err), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out.Text}}}, nil
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: tools.ErrorText(err)}},
	}
}

// unknownToolResult は、その窓口に出ていないツールの tools/call への応答。
// JSON-RPC の -32601 は SDK が本文を「method not found」に書き換えてしまいコードが伝わらないため、
// ツールの失敗（isError: true）として unknown_tool の本文（API 仕様 §3.1）を返す。何も実行しない。
func unknownToolResult() *mcp.CallToolResult { return errorResult(apperr.New(apperr.UnknownTool)) }
