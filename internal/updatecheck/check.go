package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// 確認先（FR-UPD-1）。owner・repo は定数で、設定で変えられない。
const (
	Owner = "ousiassllc"
	Repo  = "swarm-console"

	apiHost       = "api.github.com"
	latestURL     = "https://" + apiHost + "/repos/" + Owner + "/" + Repo + "/releases/latest"
	defaultWait   = 10 * time.Second
	maxBodyBytes  = 1 << 20
	maxRedirects  = 5
	acceptHeader  = "application/vnd.github+json"
	rateLimitZero = "0"
)

// Status は確認の結果。Pending と Disabled は Runner だけが返す（確認の結果ではない）。
type Status string

// Status の値（API 仕様 §5.3）。
const (
	UpToDate        Status = "up_to_date"
	UpdateAvailable Status = "update_available"
	Unknown         Status = "unknown"
	Pending         Status = "pending"
	Disabled        Status = "disabled"
)

// Reason は Unknown の理由（FR-UPD-4）。URL・ヘッダー・応答本文・生のエラー文面は保持しない（FR-UPD-5）。
type Reason string

// Reason の値。
const (
	ReasonNetwork        Reason = "network"
	ReasonRateLimit      Reason = "rate_limit"
	ReasonAccess         Reason = "access"
	ReasonNoRelease      Reason = "no_release"
	ReasonReleaseInvalid Reason = "release_invalid"
	ReasonDevBuild       Reason = "dev_build"
)

// Result は 1 回の確認の結果。Latest は ValidTag を通った版だけ入る。
type Result struct {
	Status Status
	Reason Reason // Status が Unknown のときだけ
	Latest string
}

// Options は Checker の入力。
type Options struct {
	// Current は動いているバイナリの版（`dev` もありうる）。
	Current string
	// Transport は HTTP の通信路。nil なら標準。テストが偽の配信元へ向けるために差し替える。
	// リダイレクトの制限・タイムアウト・ヘッダーは Transport によらず Checker が決める。
	Transport http.RoundTripper
	// Timeout は 1 回の確認の上限。0 なら 10 秒（FR-UPD-1）。
	Timeout time.Duration
}

// Checker は releases/latest を 1 回取って結果にする。
type Checker struct {
	current string
	client  *http.Client
}

// New は Checker を作る。
func New(o Options) *Checker {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultWait
	}
	return &Checker{current: o.Current, client: &http.Client{
		Transport: o.Transport, Timeout: timeout, CheckRedirect: redirectOnlyAPI,
	}}
}

// redirectOnlyAPI は api.github.com の https の外へ追わず、5 回を超えたら止める（FR-UPD-1）。
func redirectOnlyAPI(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errors.New("リダイレクトが多すぎます")
	}
	if req.URL.Scheme != "https" || req.URL.Hostname() != apiHost {
		return errors.New("api.github.com の外へのリダイレクトは追いません")
	}
	return nil
}

// Check は 1 回確認する。現在の版が比較できない形（dev など）なら通信せず dev_build。
// ctx が終わると通信を中断する（結果は network になるので、呼び出し側は ctx.Err() を見て捨てる）。
func (c *Checker) Check(ctx context.Context) Result {
	if !ValidCurrent(c.current) {
		return Result{Status: Unknown, Reason: ReasonDevBuild}
	}
	body, reason := c.fetch(ctx)
	if reason != "" {
		return Result{Status: Unknown, Reason: reason}
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil || !ValidTag(rel.TagName) {
		return Result{Status: Unknown, Reason: ReasonReleaseInvalid}
	}
	if Newer(rel.TagName, c.current) {
		return Result{Status: UpdateAvailable, Latest: rel.TagName}
	}
	return Result{Status: UpToDate, Latest: rel.TagName}
}

// fetch は応答本文（1 MiB まで）を返す。失敗は reason だけ返す（エラー文面は捨てる。FR-UPD-5）。
func (c *Checker) fetch(ctx context.Context) ([]byte, Reason) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, http.NoBody)
	if err != nil {
		return nil, ReasonNetwork
	}
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("User-Agent", "swarm-console/"+c.current)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, ReasonNetwork
	}
	defer func() { _ = resp.Body.Close() }()
	if reason := classify(resp); reason != "" {
		return nil, reason
	}
	// 上限を超える本文は読み切らず、JSON として壊れた（release_invalid）ものとして扱う。
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, ReasonNetwork
	}
	if len(body) > maxBodyBytes {
		return nil, ReasonReleaseInvalid
	}
	return body, ""
}

// classify は 200 以外の応答を reason にする（FR-UPD-4）。200 は空を返す。
func classify(resp *http.Response) Reason {
	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		return ""
	case code == http.StatusNotFound:
		return ReasonNoRelease
	case code == http.StatusTooManyRequests:
		return ReasonRateLimit
	case code == http.StatusForbidden:
		if strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == rateLimitZero || resp.Header.Get("Retry-After") != "" {
			return ReasonRateLimit
		}
		return ReasonAccess
	case code == http.StatusUnauthorized, code >= 400 && code < 500:
		return ReasonAccess
	}
	return ReasonNetwork // 5xx と、追わなかったリダイレクトなど
}
