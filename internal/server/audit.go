package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/mcpapi"
)

// AuditFunc は mcpapi.Options.Audit に渡す記録関数。通ったリクエストごとに 1 行（slog の JSON Lines）を出す。
// 記録するのは クライアント名・窓口・メソッド・ツール名（など）・ペイン・結果コード・所要時間だけ
// （FR-SAFE-8・NFR-SEC-12）。送った本文・トークン・メモの内容・Authorization ヘッダーは扱わない。
func AuditFunc(l *slog.Logger) func(context.Context, mcpapi.Event) {
	return func(ctx context.Context, e mcpapi.Event) {
		attrs := []slog.Attr{
			slog.String("client", e.Client.Name),
			slog.String("endpoint", e.Endpoint),
			slog.String("method", e.Method),
			slog.String("code", e.Code),
			slog.Int64("duration_ms", e.Duration.Milliseconds()),
		}
		if e.Target != "" {
			attrs = append(attrs, slog.String("tool", e.Target))
		}
		if e.Pane != "" {
			attrs = append(attrs, slog.String("pane", e.Pane))
		}
		if e.Session != "" {
			attrs = append(attrs, slog.String("session", e.Session))
		}
		l.LogAttrs(ctx, slog.LevelInfo, "audit", attrs...)
	}
}

// auditHTTP は MCP に届く前に拒否したリクエスト（401・403・404・405・413）の記録。
func (s *Server) auditHTTP(endpoint, client string, status int, code apperr.Code, d time.Duration) {
	s.logger.LogAttrs(context.Background(), slog.LevelInfo, "audit",
		slog.String("client", client),
		slog.String("endpoint", endpoint),
		slog.Int("status", status),
		slog.String("code", string(code)),
		slog.Int64("duration_ms", d.Milliseconds()),
	)
}
