package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"
)

// Secret はログ・エラー文・デバッグ出力に値を出してはいけない設定値（webhook の URL・署名の鍵・トークン）。
// fmt・slog・errors に渡しても伏せ字になり、値が要る送信の場所だけが Reveal で取り出す（NFR-SEC-18）。
type Secret string

// 伏せ字（未設定の値は空のまま見せる）。
const redacted = "[redacted]"

// Reveal は値そのもの。送信の組み立て以外では呼ばない。
func (s Secret) Reveal() string { return string(s) }

func (s Secret) mask() string {
	if s == "" {
		return ""
	}
	return redacted
}

// String は fmt の %s・%v に使われる。
func (s Secret) String() string { return s.mask() }

// GoString は fmt の %#v に使われる。
func (s Secret) GoString() string { return `"` + s.mask() + `"` }

// LogValue は slog の属性に使われる。
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.mask()) }

// MarshalText は encoding/json などで書き出すときに使われる。
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.mask()), nil }

// webhook の値の範囲と既定（FR-STATE-1〜7）。
const (
	defaultWebhookMaxAttempts   = 5
	maxWebhookMaxAttempts       = 10
	defaultWebhookRetryInterval = 30 * time.Second
	minWebhookRetryInterval     = time.Second
	MaxWebhookRetryInterval     = time.Hour // 再送の間隔の上限（倍々にしてもこれを超えない）
	defaultWebhookTimeout       = 10 * time.Second
	minWebhookTimeout           = time.Second
	maxWebhookTimeout           = time.Minute
)

// Webhook は [webhook]。URL が空ならオフ（既定。何も送らない）。
// 秘密の値は Secret に入れ、値を出す場所を Reveal だけにする。
type Webhook struct {
	URL           Secret        // 送信先。URL にトークンが入る受け手もあるので、値は出さない
	Secret        Secret        // 空でなければ、本文の HMAC-SHA256 を X-Swarm-Console-Signature に付ける
	Token         Secret        // 空でなければ Authorization: Bearer に付ける
	MaxAttempts   int           // 最初の 1 回を含む送信の上限回数（1〜10）
	RetryInterval time.Duration // 最初の再送までの間隔。以後は倍々で、上限は MaxWebhookRetryInterval
	Timeout       time.Duration // 1 回の送信の上限
}

// Enabled は送信先が設定されているか（設定したときだけ送る）。
func (w Webhook) Enabled() bool { return w.URL != "" }

type rawWebhook struct {
	URL           string  `toml:"url"`
	Secret        string  `toml:"secret"`
	Token         string  `toml:"token"`
	MaxAttempts   *int    `toml:"max_attempts"`
	RetryInterval *string `toml:"retry_interval"`
	Timeout       *string `toml:"timeout"`
}

// buildWebhook は [webhook] を既定値つきで検査する。エラー文に値（URL・鍵・トークン）は入れない。
func buildWebhook(r rawWebhook) (Webhook, error) {
	w := Webhook{
		URL: Secret(r.URL), Secret: Secret(r.Secret), Token: Secret(r.Token),
		MaxAttempts: defaultWebhookMaxAttempts, RetryInterval: defaultWebhookRetryInterval, Timeout: defaultWebhookTimeout,
	}
	if err := checkWebhookURL(r.URL); err != nil {
		return Webhook{}, err
	}
	for _, c := range []struct{ key, value string }{{"webhook.secret", r.Secret}, {"webhook.token", r.Token}} { // 固定の順（エラーの出方を毎回同じにする）
		if err := CheckPlainLine(c.key, c.value); err != nil {
			return Webhook{}, err
		}
	}
	if r.MaxAttempts != nil {
		if *r.MaxAttempts < 1 || *r.MaxAttempts > maxWebhookMaxAttempts {
			return Webhook{}, fmt.Errorf("webhook.max_attempts は 1 以上 %d 以下にしてください", maxWebhookMaxAttempts)
		}
		w.MaxAttempts = *r.MaxAttempts
	}
	var err error
	if w.RetryInterval, err = webhookDuration("webhook.retry_interval", r.RetryInterval, w.RetryInterval, minWebhookRetryInterval, MaxWebhookRetryInterval); err != nil {
		return Webhook{}, err
	}
	if w.Timeout, err = webhookDuration("webhook.timeout", r.Timeout, w.Timeout, minWebhookTimeout, maxWebhookTimeout); err != nil {
		return Webhook{}, err
	}
	return w, nil
}

func webhookDuration(key string, src *string, def, lo, hi time.Duration) (time.Duration, error) {
	if src == nil {
		return def, nil
	}
	d, err := time.ParseDuration(*src)
	if err != nil {
		return 0, fmt.Errorf("%s は 5s・2m のような期間で書いてください", key)
	}
	if d < lo || d > hi {
		return 0, fmt.Errorf("%s は %s 以上 %s 以下にしてください", key, lo, hi)
	}
	return d, nil
}

// checkWebhookURL は webhook.url が https（http はループバックのアドレス 127.0.0.0/8・::1 と localhost だけ）で、
// ユーザー情報を含まないことを確かめる。https はどのホストでも受け付ける（ループバック・リンクローカル・プライベートの
// アドレスを指す URL を書かないのは、利用者の責任）。空はオフ。url.Parse のエラーは値を含むので、そのまま返さない。
func checkWebhookURL(s string) error {
	if s == "" {
		return nil
	}
	bad := errors.New("webhook.url は https://… の URL にしてください（http はループバックのアドレス（127.0.0.0/8・::1）と localhost だけ。ユーザー名・パスワードは URL に書けません）")
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil {
		return bad
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
	}
	return bad
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
