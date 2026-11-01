package selfupdate

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// ダウンロードの上限（FR-UPD-13。P-57）。
const (
	maxSumsBytes   = 64 << 10
	maxBinaryBytes = 64 << 20
	maxRedirects   = 5
	connectTimeout = 10 * time.Second
	totalTimeout   = 5 * time.Minute
)

// allowedHosts はリリースの実体を置く GitHub のホスト（リダイレクト先を含む。NFR-SEC-15）。
var allowedHosts = map[string]bool{
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// newClient はダウンロード用の HTTP クライアント。リダイレクトは許可ホストの https だけ、5 回まで。
// transport が nil なら、接続 10 秒の標準の通信路。認証は付けない。
func newClient(transport http.RoundTripper, timeout time.Duration) *http.Client {
	if transport == nil {
		transport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: connectTimeout}).DialContext,
			TLSHandshakeTimeout: connectTimeout,
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: allowRedirect}
}

func allowRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errors.New("リダイレクトが多すぎます")
	}
	if !allowedURL(req) {
		return errors.New("許可されないリダイレクト先です")
	}
	return nil
}

func allowedURL(req *http.Request) bool {
	u := req.URL
	return u.Scheme == "https" && allowedHosts[u.Hostname()] && (u.Port() == "" || u.Port() == "443") && u.User == nil
}

// download は url を GET し、200 の本文を返す（Body は呼び出し側が閉じる）。
// 404 は asset_missing、それ以外の失敗は download。ctx が終わっていれば ctx.Err() を返す（SIGTERM での中断）。
func download(ctx context.Context, c *http.Client, url string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fail(ReasonDownload)
	}
	if !allowedURL(req) {
		return nil, fail(ReasonDownload)
	}
	req.Header.Set("User-Agent", "swarm-console")
	resp, err := c.Do(req)
	if err != nil {
		return nil, abortOr(ctx, ReasonDownload)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, fail(ReasonAssetMissing)
	case resp.StatusCode != http.StatusOK, resp.ContentLength > limit:
		_ = resp.Body.Close()
		return nil, fail(ReasonDownload)
	}
	return resp.Body, nil
}

// abortOr は呼び出し元から中断されていれば context.Canceled（SIGTERM。失敗として数えない）、
// そうでなければ（全体のタイムアウトを含む）reason の失敗にする。
func abortOr(ctx context.Context, r Reason) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return fail(r)
}

// readLimited は r を limit バイトまで読む。超えたら download。
func readLimited(ctx context.Context, r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, abortOr(ctx, ReasonDownload)
	}
	if int64(len(b)) > limit {
		return nil, fail(ReasonDownload)
	}
	return b, nil
}
