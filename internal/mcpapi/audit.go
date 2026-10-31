package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

const codeOK = "ok"

// middleware は要求ごとに、窓口に出ていないツールの拒否・resources/list の動的な列挙・監査ログの記録を行う。
func (b *builder) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		start := time.Now()
		ev := Event{Endpoint: b.endpoint, Method: method}
		ev.Target, ev.Pane = describe(req)
		var (
			res mcp.Result
			err error
		)
		switch {
		case method == "tools/call" && !b.hasTool(ev.Target):
			res = unknownToolResult()
		case method == "resources/read" && !validResourceRequest(req):
			err = resourceNotFound(apperr.New(apperr.PaneNotFound))
		case method == "resources/list":
			res, err = b.listResources(ctx)
		default:
			res, err = next(ctx, method, req)
		}
		if b.o.Audit != nil {
			ev.Client, _ = auth.FromContext(ctx)
			ev.Code, ev.Duration = codeOf(res, err), time.Since(start)
			b.o.Audit(ctx, ev)
		}
		return res, err
	}
}

func (b *builder) hasTool(name string) bool {
	_, ok := b.o.Tools.Lookup(b.endpoint, name)
	return ok
}

// describe は監査ログに載せる対象（ツール名・prompt 名・resource 名）と pane を要求から取り出す。
// 引数の本文・メモの内容など、pane 以外の値は見ない。
func describe(req mcp.Request) (target, paneArg string) {
	switch r := req.(type) {
	case *mcp.CallToolRequest:
		if r.Params != nil {
			return r.Params.Name, argPane(r.Params.Arguments)
		}
	case *mcp.GetPromptRequest:
		if r.Params != nil {
			return r.Params.Name, r.Params.Arguments["pane"]
		}
	case *mcp.ReadResourceRequest:
		if r.Params != nil {
			kind, pos, _ := parseURI(r.Params.URI)
			return kind, pos
		}
	}
	return "", ""
}

// argPane は tools/call の引数から pane（文字列のときだけ）を取り出す。短く切って記録する。
func argPane(args json.RawMessage) string {
	var a struct {
		Pane json.RawMessage `json:"pane"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(a.Pane, &s) != nil {
		return ""
	}
	return clip(s, 40)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// codeOf は結果のコード。ツールの失敗は本文のエラーコード、JSON-RPC の失敗は data の本文か "jsonrpc_<code>"。
func codeOf(res mcp.Result, err error) string {
	if err != nil {
		var je *jsonrpc.Error
		if errors.As(err, &je) {
			if c := bodyCode(je.Data); c != "" {
				return c
			}
			return "jsonrpc_" + strconv.FormatInt(je.Code, 10)
		}
		return tools.ErrorCode(err)
	}
	if r, ok := res.(*mcp.CallToolResult); ok && r.IsError {
		if len(r.Content) > 0 {
			if t, ok := r.Content[0].(*mcp.TextContent); ok {
				if c := bodyCode(json.RawMessage(t.Text)); c != "" {
					return c
				}
			}
		}
		return "tool_error"
	}
	return codeOK
}

// bodyCode は {"error":{"code":...}} の code を取り出す。
func bodyCode(raw json.RawMessage) string {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return body.Error.Code
}

// validResourceRequest は resources/read の URI がこのサーバーの形（swarm://report・pane/<位置>・notes/<位置>）か。
// 形が違うものは SDK に渡さず、解決できない位置と同じ -32002 で返す。
func validResourceRequest(req mcp.Request) bool {
	r, ok := req.(*mcp.ReadResourceRequest)
	if !ok || r.Params == nil {
		return false
	}
	_, _, ok = parseURI(r.Params.URI)
	return ok
}
