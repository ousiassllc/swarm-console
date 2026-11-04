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
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

const codeOK = "ok"

// maxAuditPath は監査ログに残す file_get のパスの最大字数（クライアントが付けた文字列をそのまま残さない）。
const maxAuditPath = 512

// middleware は要求ごとに、窓口に出ていないツールの拒否・resources/list の動的な列挙・監査ログの記録を行う。
func (b *builder) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		start := time.Now()
		ev := Event{Endpoint: b.endpoint, Method: method}
		name, paneArg, sessionArg := describe(req)
		ev.Target, ev.Pane, ev.Session = b.auditTarget(method, name), paneArg, sessionArg
		var (
			res mcp.Result
			err error
			au  *toolkit.Audit
		)
		if method == "tools/call" {
			ctx, au = toolkit.WithAudit(ctx)
		}
		switch {
		case method == "tools/call" && !b.hasTool(name):
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
			if au != nil {
				ev.Path, ev.Size, ev.HasSize = clip(au.Path, maxAuditPath), au.Size, au.HasSize
			}
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

// unknownTarget は登録されていないツール名・prompt 名の代わりに監査ログへ記録する固定値。
// クライアントが付けた文字列（最大 256 KiB）をそのまま残さない。
const unknownTarget = "unknown"

// auditTarget は監査ログに載せるツール名・prompt 名。この窓口に登録されたものだけ記録し、ほかは unknown にする。
// resources/read の種別（report|pane|notes）は parseURI が決める固定値なのでそのまま。
func (b *builder) auditTarget(method, name string) string {
	switch method {
	case "tools/call":
		if b.hasTool(name) {
			return name
		}
		return unknownTarget
	case "prompts/get":
		if b.hasPrompt(name) {
			return name
		}
		return unknownTarget
	}
	return name
}

// hasPrompt は name がこの窓口に登録された prompt か（/report は report のみ、/ops は report と dispatch）。
func (b *builder) hasPrompt(name string) bool {
	return name == "report" || (name == "dispatch" && b.endpoint == toolkit.EndpointOps)
}

// describe は要求からツール名・prompt 名・resource 種別と、pane・session（短く切ったもの）を取り出す。
// 名前はクライアントの入力のままなので、記録の前に auditTarget を通す。
// 引数の本文・メモの内容など、pane・session 以外の値は見ない。
func describe(req mcp.Request) (target, paneArg, sessionArg string) {
	switch r := req.(type) {
	case *mcp.CallToolRequest:
		if r.Params != nil {
			return r.Params.Name, argString(r.Params.Arguments, "pane"), argString(r.Params.Arguments, "session")
		}
	case *mcp.GetPromptRequest:
		if r.Params != nil {
			return r.Params.Name, clip(r.Params.Arguments["pane"], 40), clip(r.Params.Arguments["session"], 40)
		}
	case *mcp.ReadResourceRequest:
		if r.Params != nil {
			kind, pos, _ := parseURI(r.Params.URI)
			return kind, clip(pos, 40), ""
		}
	}
	return "", "", ""
}

// argString は tools/call の引数から key（文字列のときだけ）を取り出す。短く切って記録する。
func argString(args json.RawMessage, key string) string {
	var a map[string]json.RawMessage
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(a[key], &s) != nil {
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
		return toolkit.ErrorCode(err)
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
