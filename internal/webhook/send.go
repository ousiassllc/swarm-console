package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/ousiassllc/swarm-console/internal/config"
)

// 失敗の理由。固定の短い語だけをログに出す（エラーの文面には URL・ホスト名が入りうるので、そのまま出さない）。
const (
	reasonStatus   = "http_status"
	reasonTimeout  = "timeout"
	reasonCanceled = "canceled"
	reasonTLS      = "tls"
	reasonDNS      = "dns"
	reasonConnect  = "connect"
	reasonRequest  = "request"
	// reasonExhausted は、読み込んだ項目がすでに上限まで送っていたので、送らずに諦めたこと。
	reasonExhausted = "attempts_exhausted"
	// reasonNoTarget は、台帳から読んだ項目のセッションに、今の設定では送り先が無いので、送らずに諦めたこと。
	reasonNoTarget = "no_destination"
)

// 署名・認証のヘッダー名。値はログに出さない。
const (
	headerSignature = "X-Swarm-Console-Signature" // sha256=<本文の HMAC-SHA256（hex）>
	headerEventID   = "X-Swarm-Console-Event-Id"
	maxDrainBytes   = 64 << 10
)

// errGaveUp は再送の上限まで（または再送しても無駄な応答で）送れなかったこと。理由はログに残してある。
var errGaveUp = errors.New("webhook を送れませんでした")

// newClient は設定した URL だけに送るクライアント。リダイレクトは追わない（Authorization を別の宛先へ送らないため）。
func newClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// send は項目の本文を、送れる（2xx）か諦めるまで POST する。送った回数の上限は MaxAttempts（最初の 1 回を含み、
// 再起動をまたいで数える）で、間隔は RetryInterval から倍々（上限 config.MaxWebhookRetryInterval）。
// 送れたら nil、諦めたら errGaveUp（理由はログに残す）、ctx が終わったらそのエラー。
func (n *Notifier) send(ctx context.Context, it *item) error {
	var pl Payload
	if err := json.Unmarshal(it.Payload, &pl); err != nil {
		n.o.Logger.Error("webhook の本文を読めませんでした", "event_id", it.EventID)
		return errGaveUp
	}
	cfg, ok := n.o.Config.Target(pl.Session)
	if !ok { // 再起動で設定が変わり、台帳の件のセッションに送り先が無くなった
		n.o.Logger.Error("webhook を送れませんでした", "event_id", it.EventID, "session", pl.Session, "pane", pl.Pane, "reason", reasonNoTarget)
		return errGaveUp
	}
	// 再起動で引き継いだ件は、送った回数に応じた間隔をあけてから続ける（再送の待ちの途中で止まったときの続き）。
	if it.Attempts > 0 && it.Attempts < cfg.MaxAttempts {
		if err := n.o.Sleep(ctx, retryDelay(cfg.RetryInterval, it.Attempts)); err != nil {
			return err
		}
	}
	for {
		attempt, err := n.beginAttempt(ctx, it, cfg.MaxAttempts)
		if errors.Is(err, errGaveUp) {
			n.o.Logger.Error("webhook を送れませんでした", "event_id", it.EventID, "session", pl.Session, "pane", pl.Pane,
				"attempts", attempt, "reason", reasonExhausted)
			return errGaveUp
		}
		if err != nil {
			return err
		}
		status, reason := n.post(ctx, cfg, it.EventID, it.Payload)
		if reason == "" {
			n.o.Logger.Info("webhook を送りました", "event_id", it.EventID, "session", pl.Session, "pane", pl.Pane, "attempt", attempt, "status", status)
			return nil
		}
		if err := ctx.Err(); err != nil {
			n.refundAttempt(it) // 終了で中断した通信は、送った回数に数えない（再起動後に、その分をやり直す）
			return err
		}
		if attempt >= cfg.MaxAttempts || !retryable(status, reason) {
			n.o.Logger.Error("webhook を送れませんでした", "event_id", it.EventID, "session", pl.Session, "pane", pl.Pane,
				"attempts", attempt, "reason", reason, "status", status)
			return errGaveUp
		}
		delay := retryDelay(cfg.RetryInterval, attempt)
		n.o.Logger.Warn("webhook の送信に失敗しました。あとで再送します", "event_id", it.EventID, "session", pl.Session, "pane", pl.Pane,
			"attempt", attempt, "max_attempts", cfg.MaxAttempts, "retry_in", delay.String(), "reason", reason, "status", status)
		if err := n.o.Sleep(ctx, delay); err != nil {
			return err
		}
	}
}

// retryDelay は attempt 回目の失敗のあとの待ち。RetryInterval から倍々で、上限は config.MaxWebhookRetryInterval。
// 再起動後に続けるときも、送った回数から同じ値になる。
func retryDelay(base time.Duration, attempt int) time.Duration {
	d := base
	for i := 1; i < attempt && d < config.MaxWebhookRetryInterval; i++ {
		d *= 2
	}
	return min(d, config.MaxWebhookRetryInterval)
}

// post は 1 回送る。成功なら reason は ""。失敗の理由は固定の語で返し、err の文面は使わない。
func (n *Notifier) post(ctx context.Context, cfg config.Webhook, id string, body []byte) (status int, reason string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL.Reveal(), bytes.NewReader(body))
	if err != nil {
		return 0, reasonRequest
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "swarm-console/"+n.o.Version)
	req.Header.Set(headerEventID, id)
	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret.Reveal()))
		mac.Write(body)
		req.Header.Set(headerSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token.Reveal())
	}
	client := n.o.Client
	if client == nil {
		client = newClient(cfg.Timeout) // Transport は共通（http.DefaultTransport）なので、接続は使い回される
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, classify(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, ""
	}
	return resp.StatusCode, reasonStatus
}

// retryable は再送して意味がある失敗か。通信の失敗（接続・名前解決・TLS・タイムアウト・応答なしで切れた）と、
// 応答の 408・425・429・5xx は再送する。そのほかの 4xx・3xx（リダイレクトは追わない）と、リクエストを作れない失敗
// （reasonRequest。URL の誤りなど）は、設定の誤りなので再送しない。
func retryable(status int, reason string) bool {
	switch reason {
	case reasonConnect, reasonDNS, reasonTimeout, reasonTLS:
		return true
	case reasonStatus:
		switch status {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
			return true
		}
		return status >= 500
	}
	return false
}

// classify は client.Do のエラーを、URL・ホスト名を含まない固定の理由にする。取り消し・タイムアウト・TLS・名前解決・
// 接続のほか、分類できない通信の失敗（EOF・接続のリセットなど）も reasonConnect にして再送する
// （リクエストを作れない失敗だけが reasonRequest で、それは post が直接返す）。
func classify(err error) string {
	var certErr *tls.CertificateVerificationError
	var dnsErr *net.DNSError
	var opErr *net.OpError
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return reasonCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return reasonTimeout
	case errors.As(err, &certErr):
		return reasonTLS
	case errors.As(err, &dnsErr):
		return reasonDNS
	case errors.As(err, &netErr) && netErr.Timeout():
		return reasonTimeout
	case errors.As(err, &opErr):
		return reasonConnect
	}
	return reasonConnect
}

// sleepCtx は d 待つ。ctx が終わったらそのエラーで戻る。
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
