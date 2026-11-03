package tools

import "context"

// Audit はツールが監査ログへ足す値（file_get の取得先とサイズ）。mcpapi が tools/call ごとに用意し、ハンドラが埋める。
// 本文（base64 など）は入れない。
type Audit struct {
	Path    string // 実パス（解決できたとき）。解決前に断ったときは入力のまま
	Size    int64
	HasSize bool
}

type auditKey struct{}

// WithAudit は監査ログ用の Audit を ctx に載せる。tools/call の前に mcpapi が付け、呼び出しのあとで中身を読む。
func WithAudit(ctx context.Context) (context.Context, *Audit) {
	a := &Audit{}
	return context.WithValue(ctx, auditKey{}, a), a
}

// auditOf は ctx の Audit。無ければ nil（nil への書き込みは何もしない）。
func auditOf(ctx context.Context) *Audit {
	a, _ := ctx.Value(auditKey{}).(*Audit)
	return a
}

func (a *Audit) setPath(p string) {
	if a != nil {
		a.Path = p
	}
}

func (a *Audit) setSize(n int64) {
	if a != nil {
		a.Size, a.HasSize = n, true
	}
}
