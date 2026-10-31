package mcpapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// resource の URI（API 仕様 §6.2）。
const (
	uriReport    = "swarm://report"
	uriPanePfx   = "swarm://pane/"
	uriNotesPfx  = "swarm://notes/"
	mimeMarkdown = "text/markdown"
	mimeJSON     = "application/json"

	// codeResourceNotFound は解決できない位置の JSON-RPC エラーコード（API 仕様 §6.2）。
	codeResourceNotFound = -32002
)

// parseURI は resource の URI を種別（report|pane|notes）と位置に分ける。% は %25 でエスケープされている。
// 知らない URI は kind が ""。
func parseURI(uri string) (kind, pos string, ok bool) {
	if uri == uriReport {
		return "report", "", true
	}
	for _, p := range []struct{ pfx, kind string }{{uriPanePfx, "pane"}, {uriNotesPfx, "notes"}} {
		if rest, found := strings.CutPrefix(uri, p.pfx); found {
			pos, err := url.PathUnescape(rest)
			if err != nil || pos == "" {
				return p.kind, "", false
			}
			return p.kind, clip(pos, 40), true
		}
	}
	return "", "", false
}

// escapePos は位置・ID を URI のパス要素にする（% → %25）。
func escapePos(pos string) string { return url.PathEscape(pos) }

func (b *builder) addResources(s *mcp.Server) {
	s.AddResource(&mcp.Resource{
		Name: "report", URI: uriReport, MIMEType: mimeMarkdown,
		Description: "全ペインの状況（report ツールの markdown・既定言語と同じ）",
	}, b.readResource)
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "pane", URITemplate: uriPanePfx + "{pos}", MIMEType: mimeMarkdown,
		Description: "1 ペインの詳しい版（report の pane 指定と同じ）。位置または tmux ID（% は %25）",
	}, b.readResource)
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		Name: "notes", URITemplate: uriNotesPfx + "{pos}", MIMEType: mimeJSON,
		Description: "1 ペインのメモ（note_get と同じ）。位置または tmux ID（% は %25）",
	}, b.readResource)
}

func (b *builder) readResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	kind, pos, ok := parseURI(uri)
	if !ok {
		return nil, resourceNotFound(apperr.New(apperr.PaneNotFound))
	}
	var (
		text string
		mime = mimeMarkdown
		err  error
	)
	switch kind {
	case "report":
		text, err = b.o.Tools.ReportMarkdown(ctx, "")
	case "pane":
		text, err = b.o.Tools.ReportMarkdown(ctx, pos)
	default:
		text, err = b.o.Tools.NoteJSON(ctx, pos)
		mime = mimeJSON
	}
	if err != nil {
		return nil, resourceNotFound(err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mime, Text: text}}}, nil
}

// resourceNotFound は resource の失敗を JSON-RPC エラーにする。pane_not_found は -32002、それ以外は内部エラー。
// どちらも API 仕様 §3.1 の本文を data に入れる。
func resourceNotFound(err error) error {
	e := apperr.As(err)
	code := int64(jsonrpc.CodeInternalError)
	if e.Code == apperr.PaneNotFound {
		code = codeResourceNotFound
	}
	return &jsonrpc.Error{Code: code, Message: e.Message(), Data: json.RawMessage(tools.ErrorText(e))}
}

// listResources は resources/list の応答。swarm://report と、現在のペインぶんの pane・notes の URI を位置順に並べる。
func (b *builder) listResources(ctx context.Context) (*mcp.ListResourcesResult, error) {
	panes, err := b.o.Tools.Panes(ctx)
	if err != nil {
		return nil, resourceNotFound(err)
	}
	out := []*mcp.Resource{{
		Name: "report", URI: uriReport, MIMEType: mimeMarkdown, Description: "全ペインの状況",
	}}
	for _, p := range panes {
		out = append(out,
			&mcp.Resource{Name: "pane " + p.Pos, URI: uriPanePfx + escapePos(p.Pos), MIMEType: mimeMarkdown, Description: "ペイン " + p.Pos + " の詳しい版"},
			&mcp.Resource{Name: "notes " + p.Pos, URI: uriNotesPfx + escapePos(p.Pos), MIMEType: mimeJSON, Description: "ペイン " + p.Pos + " のメモ"},
		)
	}
	return &mcp.ListResourcesResult{Resources: out}, nil
}
