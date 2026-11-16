package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
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
// [webhook.sessions."<名前>"] があるセッションは、その表の送り先を使う（Sessions。Target で引く）。
type Webhook struct {
	URL           Secret        // 送信先。URL にトークンが入る受け手もあるので、値は出さない
	Secret        Secret        // 空でなければ、本文の HMAC-SHA256 を X-Swarm-Console-Signature に付ける
	Token         Secret        // 空でなければ Authorization: Bearer に付ける
	MaxAttempts   int           // 最初の 1 回を含む送信の上限回数（1〜10）
	RetryInterval time.Duration // 最初の再送までの間隔。以後は倍々で、上限は MaxWebhookRetryInterval
	Timeout       time.Duration // 1 回の送信の上限

	// Sessions はセッション名ごとの送り先。セッションの表を書いたセッションだけが入る（url が空の表は、そのセッションだけオフ）。
	// 各値の Sessions は常に nil。
	Sessions map[string]Webhook
}

// Enabled は送信先が 1 つでも設定されているか（設定したときだけ送る）。
func (w Webhook) Enabled() bool {
	if w.URL != "" {
		return true
	}
	for _, s := range w.Sessions {
		if s.URL != "" {
			return true
		}
	}
	return false
}

// Target はセッションの送り先。セッションの表があればそれ（url が空ならなし）、なければ [webhook]（url が空ならなし）。
// 戻り値の ok が false なら、そのセッションは何も送らない。
func (w Webhook) Target(session string) (t Webhook, ok bool) {
	if s, found := w.Sessions[session]; found {
		return s, s.URL != ""
	}
	return w, w.URL != ""
}

// rawWebhookTarget は送り先 1 つ分のキー（[webhook] 自体と、[webhook.sessions."<名前>"] で共通）。
type rawWebhookTarget struct {
	URL           string  `toml:"url"`
	Secret        string  `toml:"secret"`
	Token         string  `toml:"token"`
	MaxAttempts   *int    `toml:"max_attempts"`
	RetryInterval *string `toml:"retry_interval"`
	Timeout       *string `toml:"timeout"`
}

type rawWebhook struct {
	URL           string                      `toml:"url"`
	Secret        string                      `toml:"secret"`
	Token         string                      `toml:"token"`
	MaxAttempts   *int                        `toml:"max_attempts"`
	RetryInterval *string                     `toml:"retry_interval"`
	Timeout       *string                     `toml:"timeout"`
	Sessions      map[string]rawWebhookTarget `toml:"sessions"`
}

func (r rawWebhook) target() rawWebhookTarget {
	return rawWebhookTarget{URL: r.URL, Secret: r.Secret, Token: r.Token, MaxAttempts: r.MaxAttempts, RetryInterval: r.RetryInterval, Timeout: r.Timeout}
}

// buildWebhook は [webhook] と [webhook.sessions."<名前>"] を既定値つきで検査する。sessions は設定の sessions
// （表の名前はこの中のものだけ）。セッションの表は、再送の設定（max_attempts・retry_interval・timeout）を書かなければ
// [webhook] の値を受け継ぐ。url・secret・token は受け継がない（別の受け手へ [webhook] の鍵・トークンを送らないため）。
// エラー文に値（URL・鍵・トークン）は入れない。
func buildWebhook(r rawWebhook, sessions []string) (Webhook, error) {
	def := Webhook{MaxAttempts: defaultWebhookMaxAttempts, RetryInterval: defaultWebhookRetryInterval, Timeout: defaultWebhookTimeout}
	w, err := buildWebhookTarget("webhook", r.target(), def)
	if err != nil {
		return Webhook{}, err
	}
	if len(r.Sessions) == 0 {
		return w, nil
	}
	names := make([]string, 0, len(r.Sessions))
	for name := range r.Sessions {
		names = append(names, name)
	}
	slices.Sort(names) // エラーの出方を毎回同じにする
	w.Sessions = make(map[string]Webhook, len(names))
	for _, name := range names {
		if !slices.Contains(sessions, name) {
			return Webhook{}, fmt.Errorf("webhook.sessions に sessions にない名前があります（%q。sessions: %q）", name, sessions)
		}
		st, err := buildWebhookTarget(fmt.Sprintf("webhook.sessions.%q", name), r.Sessions[name], Webhook{
			MaxAttempts: w.MaxAttempts, RetryInterval: w.RetryInterval, Timeout: w.Timeout,
		})
		if err != nil {
			return Webhook{}, err
		}
		w.Sessions[name] = st
	}
	return w, nil
}

// buildWebhookTarget は送り先 1 つを検査する。key はエラー文に入れるキーの接頭辞、def は書かなかった再送の設定の既定。
func buildWebhookTarget(key string, r rawWebhookTarget, def Webhook) (Webhook, error) {
	w := Webhook{URL: Secret(r.URL), Secret: Secret(r.Secret), Token: Secret(r.Token), MaxAttempts: def.MaxAttempts, RetryInterval: def.RetryInterval, Timeout: def.Timeout}
	if err := checkWebhookURL(key+".url", r.URL); err != nil {
		return Webhook{}, err
	}
	for _, c := range []struct{ key, value string }{{key + ".secret", r.Secret}, {key + ".token", r.Token}} { // 固定の順（エラーの出方を毎回同じにする）
		if err := CheckPlainLine(c.key, c.value); err != nil {
			return Webhook{}, err
		}
	}
	if r.MaxAttempts != nil {
		if *r.MaxAttempts < 1 || *r.MaxAttempts > maxWebhookMaxAttempts {
			return Webhook{}, fmt.Errorf("%s.max_attempts は 1 以上 %d 以下にしてください", key, maxWebhookMaxAttempts)
		}
		w.MaxAttempts = *r.MaxAttempts
	}
	var err error
	if w.RetryInterval, err = webhookDuration(key+".retry_interval", r.RetryInterval, w.RetryInterval, minWebhookRetryInterval, MaxWebhookRetryInterval); err != nil {
		return Webhook{}, err
	}
	if w.Timeout, err = webhookDuration(key+".timeout", r.Timeout, w.Timeout, minWebhookTimeout, maxWebhookTimeout); err != nil {
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

// checkWebhookURL は key（webhook.url など）の URL が https（http はループバックのアドレス 127.0.0.0/8・::1 と localhost だけ）で、
// ユーザー情報を含まないことを確かめる。https はどのホストでも受け付ける（ループバック・リンクローカル・プライベートの
// アドレスを指す URL を書かないのは、利用者の責任）。空はオフ。url.Parse のエラーは値を含むので、そのまま返さない。
func checkWebhookURL(key, s string) error {
	if s == "" {
		return nil
	}
	bad := errors.New(key + " は https://… の URL にしてください（http はループバックのアドレス（127.0.0.0/8・::1）と localhost だけ。ユーザー名・パスワードは URL に書けません）")
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
