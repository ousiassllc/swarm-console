// Package server は net/http の起動、/report・/ops のルーティング、認証、本文上限、監査ログ、グレースフル停止を担う。
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

// 既定値。
const (
	// MaxBodyBytes はリクエスト本文の上限（256 KiB。NFR-SEC-13）。
	MaxBodyBytes = 256 << 10
	// ShutdownTimeout は終了シグナルのあと、進行中の操作を待つ上限（NFR-AVL-7）。
	ShutdownTimeout = 10 * time.Second

	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 64 << 10
)

// Authenticator は Authorization ヘッダーからクライアントを決める（auth.Authenticator が実装する）。
type Authenticator interface {
	Authenticate(authorization string) (auth.Client, error)
}

// Options は New の入力。
type Options struct {
	// ListenAddr は待ち受けアドレス（tailnet の IP:ポート。config 側で検証済み）。
	ListenAddr string
	Auth       Authenticator
	// Report・Ops は窓口ごとの MCP ハンドラ（mcpapi.NewHTTPHandler）。
	Report http.Handler
	Ops    http.Handler
	// Logger は監査ログ（JSON Lines）と起動・停止のログの出力先。nil なら出さない。
	Logger *slog.Logger
	// ShutdownTimeout は 0 なら既定（10 秒）。
	ShutdownTimeout time.Duration
}

// Server は HTTP サーバー。
type Server struct {
	o         Options
	logger    *slog.Logger
	handler   http.Handler
	ready     chan struct{}
	readyOnce sync.Once
}

// New はサーバーを組み立てる。
func New(o Options) (*Server, error) {
	switch {
	case o.Auth == nil:
		return nil, errors.New("server: Auth が必要です")
	case o.Report == nil || o.Ops == nil:
		return nil, errors.New("server: Report と Ops のハンドラが必要です")
	}
	if o.ShutdownTimeout <= 0 {
		o.ShutdownTimeout = ShutdownTimeout
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{o: o, logger: logger, ready: make(chan struct{})}
	s.handler = s.routes()
	return s, nil
}

// Ready は待ち受けを始めたとき（ServeListener が受け付けを始めた直後）に閉じる channel。
// 自己更新の試運転は、ここから 60 秒数える（FR-UPD-17）。
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Handler はルーティング済みのハンドラ（httptest から使える）。
func (s *Server) Handler() http.Handler { return s.handler }

// Serve は ListenAddr で待ち受ける。待ち受けに失敗したらエラー（systemd の Restart=always に任せる）。
// ctx が終わったら新しい接続を止め、進行中の操作を最大 ShutdownTimeout 待って戻る。
func (s *Server) Serve(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.o.ListenAddr)
	if err != nil {
		return fmt.Errorf("server: %s で待ち受けられません: %w", s.o.ListenAddr, err)
	}
	return s.ServeListener(ctx, ln)
}

// ServeListener は ln で受け付ける（テスト用に Serve から分けてある）。
func (s *Server) ServeListener(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		// 進行中の操作は終了シグナルで打ち切らない。待ち時間は Shutdown の上限で決める。
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	s.logger.Info("待ち受けを開始しました", "addr", ln.Addr().String())
	s.readyOnce.Do(func() { close(s.ready) })

	select {
	case err := <-done:
		return fmt.Errorf("server: 待ち受けが止まりました: %w", err)
	case <-ctx.Done():
	}
	s.logger.Info("終了シグナルを受けました。進行中の操作を待ちます", "timeout", s.o.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.o.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		<-done
		return fmt.Errorf("server: 進行中の操作が %s で終わりませんでした: %w", s.o.ShutdownTimeout, err)
	}
	<-done
	return nil
}
