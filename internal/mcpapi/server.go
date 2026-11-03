// Package mcpapi は公式 SDK で /report・/ops の MCP サーバーを作り、窓口ごとにツール・prompts・resources を登録する。
// Streamable HTTP はステートレスで動かす（P-02。Mcp-Session-Id を持たず、GET・DELETE は 405）。
package mcpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// Event は監査ログ 1 件分（NFR-SEC-12）。送った本文・メモの内容・トークン・file_get の中身（base64）は含めない。
type Event struct {
	Endpoint string        // /report | /ops
	Client   auth.Client   // 認証済みクライアント（名前だけをログに出す）
	Method   string        // tools/call・prompts/get・resources/read など
	Target   string        // ツール名・prompt 名・resource 名（report|pane|notes）。無ければ ""
	Pane     string        // 引数・URI の pane（位置または ID）。無ければ ""
	Session  string        // 引数の session（送られたまま）。無ければ ""
	Path     string        // file_get の取得先（実パス。解決前に断ったときは入力のまま）。それ以外は ""
	Size     int64         // file_get のファイルサイズ（バイト）。HasSize が true のときだけ意味がある
	HasSize  bool          // Size が分かっているか
	Code     string        // "ok" またはエラーコード
	Duration time.Duration // 所要時間
}

// Options は NewReportServer・NewOpsServer の入力。
type Options struct {
	Tools   *tools.Registry
	Version string
	Logger  *slog.Logger // SDK 内部のログ。nil なら出さない
	// Audit は MCP の要求ごとに 1 回呼ばれる。nil なら記録しない。
	Audit func(context.Context, Event)
}

// NewReportServer は /report の MCP サーバー（見るツール 10・prompts は report のみ・resources は全部）。
func NewReportServer(o Options) *mcp.Server { return newServer(tools.EndpointReport, o) }

// NewOpsServer は /ops の MCP サーバー（見る 10 ＋ 操作 9 のツール・prompts は report と dispatch・resources は全部）。
func NewOpsServer(o Options) *mcp.Server { return newServer(tools.EndpointOps, o) }

func newServer(endpoint string, o Options) *mcp.Server {
	version := o.Version
	if version == "" {
		version = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "swarm-console", Version: version}, &mcp.ServerOptions{
		Logger: o.Logger,
		// ステートレスなので、一覧の変更通知は送れない。
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{}, Prompts: &mcp.PromptCapabilities{}, Resources: &mcp.ResourceCapabilities{},
		},
	})
	b := &builder{endpoint: endpoint, o: o}
	for _, t := range o.Tools.ForEndpoint(endpoint) {
		s.AddTool(&mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Schema}, b.toolHandler(t))
	}
	b.addPrompts(s)
	b.addResources(s)
	s.AddReceivingMiddleware(b.middleware)
	return s
}

// builder は 1 窓口ぶんの登録に使う状態。
type builder struct {
	endpoint string
	o        Options
}

// NewHTTPHandler は s をステートレス・JSON 応答の Streamable HTTP ハンドラにする。
// POST 以外（GET・DELETE）は SDK が 405 を返す。
func NewHTTPHandler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}
