// Package updatechecktest は、実際の GitHub に出ずに updatecheck・selfupdate を確かめる偽の配信元を提供する（NFR-TEST-14・16）。
// テスト専用だが、複数パッケージのテストから使うため通常のファイルに置く。
package updatechecktest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Request は偽の配信元が受けた要求（クライアントが実際に組み立てた宛先）。
type Request struct {
	Scheme string // クライアントが要求した https か
	Host   string
	Path   string
	Header http.Header
}

// Origin は 1 台の httptest サーバーが、ホスト名ごとに別のハンドラを返す偽の配信元。
// Transport をクライアントに渡すと、https://api.github.com/... のような要求がここへ届く。
type Origin struct {
	srv *httptest.Server

	mu       sync.Mutex
	handlers map[string]http.Handler
	reqs     []Request
}

// New は偽の配信元を起動する。終了時に止める。ハンドラの無いホストは 502 を返す。
func New(t testing.TB) *Origin {
	t.Helper()
	o := &Origin{handlers: map[string]http.Handler{}}
	o.srv = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(o.srv.Close)
	return o
}

// Handle は host（ポートなし。例: api.github.com）宛ての要求をハンドラ h で返す。
func (o *Origin) Handle(host string, h http.Handler) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.handlers[host] = h
}

// HandleFunc は Handle の関数版。
func (o *Origin) HandleFunc(host string, f http.HandlerFunc) { o.Handle(host, f) }

func (o *Origin) serve(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	o.mu.Lock()
	h := o.handlers[host]
	o.mu.Unlock()
	if h == nil {
		http.Error(w, "no handler for "+host, http.StatusBadGateway)
		return
	}
	h.ServeHTTP(w, r)
}

// Requests はこれまでに受けた要求（古い順）。
func (o *Origin) Requests() []Request {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Request(nil), o.reqs...)
}

// Transport はすべての要求をこの配信元へ向ける RoundTripper。Host ヘッダーは元の宛先のまま。
func (o *Origin) Transport() http.RoundTripper { return originTransport{o} }

type originTransport struct{ o *Origin }

func (t originTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.o.mu.Lock()
	t.o.reqs = append(t.o.reqs, Request{Scheme: req.URL.Scheme, Host: req.URL.Host, Path: req.URL.Path, Header: req.Header.Clone()})
	t.o.mu.Unlock()
	target, err := url.Parse(t.o.srv.URL)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.URL = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: req.URL.Path, RawQuery: req.URL.RawQuery}
	out.Host = req.URL.Host
	out.RequestURI = ""
	return http.DefaultTransport.RoundTrip(out)
}

// RoundTripFunc は関数を RoundTripper にする（接続失敗などを作る）。
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip は f を呼ぶ。
func (f RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
