package server_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/server"
)

type fakeAuth struct{}

func (fakeAuth) Authenticate(string) (auth.Client, error) {
	return auth.Client{Name: "tester", Scope: auth.ScopeOps}, nil
}

// blocking は started を知らせたあと release まで応答しない MCP ハンドラ。
type blocking struct {
	started chan struct{}
	release chan struct{}
}

func (b blocking) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	b.started <- struct{}{}
	<-b.release
	_, _ = w.Write([]byte("done"))
}

func startServer(t *testing.T, timeout time.Duration, h http.Handler) (srv *server.Server, addr string, cancel context.CancelFunc, done chan error) {
	t.Helper()
	srv, err := server.New(server.Options{
		Auth: fakeAuth{}, Report: h, Ops: h, Logger: slog.New(slog.DiscardHandler), ShutdownTimeout: timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done = make(chan error, 1)
	go func() { done <- srv.ServeListener(ctx, ln) }()
	return srv, ln.Addr().String(), cancel, done
}

type reply struct {
	body string
	err  error
}

func asyncPost(addr string) chan reply {
	ch := make(chan reply, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+"/ops", strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ch <- reply{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b := make([]byte, 16)
		n, _ := resp.Body.Read(b)
		ch <- reply{body: string(b[:n])}
	}()
	return ch
}

// NFR-AVL-7: 終了シグナルのあとも、進行中の操作が終わるまで待ってから止まる。
func TestGracefulShutdownWaitsForInFlight(t *testing.T) {
	b := blocking{started: make(chan struct{}), release: make(chan struct{})}
	_, addr, cancel, done := startServer(t, time.Minute, b)
	got := asyncPost(addr)
	<-b.started
	cancel()
	select {
	case err := <-done:
		t.Fatalf("進行中の操作があるのに止まりました: %v", err)
	default:
	}
	close(b.release)
	if r := <-got; r.err != nil || r.body != "done" {
		t.Errorf("進行中のリクエストが完了しませんでした: %+v", r)
	}
	if err := <-done; err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
	// 止まったあとは新しい接続を受け付けない。
	if canDial(addr) {
		t.Error("停止後も接続できます")
	}
}

// 待ち時間の上限（ここでは短くした値）を超えたらエラーで止まる。
func TestShutdownTimeout(t *testing.T) {
	b := blocking{started: make(chan struct{}), release: make(chan struct{})}
	_, addr, cancel, done := startServer(t, 20*time.Millisecond, b)
	got := asyncPost(addr)
	<-b.started
	cancel()
	if err := <-done; err == nil {
		t.Error("上限を超えたのにエラーになりません")
	}
	close(b.release)
	<-got
}

// tailscale が上がっていない等で待ち受けに失敗したら、エラーで戻る（systemd が再起動する）。
func TestServeListenFailure(t *testing.T) {
	busy := listen(t)
	defer func() { _ = busy.Close() }()
	h := http.NotFoundHandler()
	srv, err := server.New(server.Options{Auth: fakeAuth{}, Report: h, Ops: h, ListenAddr: busy.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(context.Background()); err == nil || !strings.Contains(err.Error(), "待ち受けられません") {
		t.Errorf("Serve = %v", err)
	}
	srv, _ = server.New(server.Options{Auth: fakeAuth{}, Report: h, Ops: h, ListenAddr: "203.0.113.1:48120"})
	if err := srv.Serve(context.Background()); err == nil {
		t.Error("持っていないアドレスで待ち受けられてしまいました")
	}
}

func TestNewRequiresHandlers(t *testing.T) {
	if _, err := server.New(server.Options{Auth: fakeAuth{}}); err == nil {
		t.Error("ハンドラなしで作れてしまいます")
	}
	if _, err := server.New(server.Options{Report: http.NotFoundHandler(), Ops: http.NotFoundHandler()}); err == nil {
		t.Error("Auth なしで作れてしまいます")
	}
}
