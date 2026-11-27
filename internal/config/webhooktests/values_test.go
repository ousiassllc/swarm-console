package webhooktests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/config"
)

const minimal = `listen_addr = "100.64.1.2:48120"` + "\n"

func parseWebhook(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	return config.ParseBytes([]byte(minimal+body), "config.toml")
}

// 書かなければオフ（既定）。URL が空なら Enabled は false。
func TestWebhookDefaultIsOff(t *testing.T) {
	cfg, err := parseWebhook(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webhook.Enabled() {
		t.Error("webhook を書いていないのに有効です")
	}
	cfg, err = parseWebhook(t, "[webhook]\nurl = \"\"\ntoken = \"x\"\n")
	if err != nil || cfg.Webhook.Enabled() {
		t.Errorf("url が空ならオフ: enabled=%v err=%v", cfg.Webhook.Enabled(), err)
	}
}

func TestWebhookValues(t *testing.T) {
	cfg, err := parseWebhook(t, `[webhook]
url = "https://hooks.example.com/t/abc"
secret = "k"
token = "tok"
max_attempts = 3
retry_interval = "2s"
timeout = "20s"
`)
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Webhook
	if !w.Enabled() || w.URL.Reveal() != "https://hooks.example.com/t/abc" || w.Secret.Reveal() != "k" || w.Token.Reveal() != "tok" ||
		w.MaxAttempts != 3 || w.RetryInterval != 2*time.Second || w.Timeout != 20*time.Second {
		t.Errorf("webhook = %+v", w)
	}
}

// 不正な値は起動拒否。エラー文に URL・鍵・トークンの値は入らない。
func TestWebhookRejectsBadValuesWithoutEchoingSecrets(t *testing.T) {
	const s = "SECRETVALUE-77"
	for name, body := range map[string]string{
		"http で外へ":       fmt.Sprintf("[webhook]\nurl = \"http://hooks.example.com/%s\"\n", s),
		"ユーザー情報":         fmt.Sprintf("[webhook]\nurl = \"https://user:%s@hooks.example.com/\"\n", s),
		"スキームなし":         fmt.Sprintf("[webhook]\nurl = \"hooks.example.com/%s\"\n", s),
		"ftp":            fmt.Sprintf("[webhook]\nurl = \"ftp://hooks.example.com/%s\"\n", s),
		"不正な URL":        fmt.Sprintf("[webhook]\nurl = \"https://hooks.example.com/%%zz%s\"\n", s),
		"token に改行":      fmt.Sprintf("[webhook]\nurl = \"https://h.example.com/\"\ntoken = \"a\\n%s\"\n", s),
		"secret に制御文字":   fmt.Sprintf("[webhook]\nurl = \"https://h.example.com/\"\nsecret = \"a\\u0007%s\"\n", s),
		"max_attempts 0": "[webhook]\nurl = \"https://h.example.com/\"\nmax_attempts = 0\n",
		"max_attempts 多": "[webhook]\nurl = \"https://h.example.com/\"\nmax_attempts = 11\n",
		"retry 短い":       "[webhook]\nurl = \"https://h.example.com/\"\nretry_interval = \"10ms\"\n",
		"retry 長い":       "[webhook]\nurl = \"https://h.example.com/\"\nretry_interval = \"2h\"\n",
		"retry 書式":       "[webhook]\nurl = \"https://h.example.com/\"\nretry_interval = \"soon\"\n",
		"timeout 長い":     "[webhook]\nurl = \"https://h.example.com/\"\ntimeout = \"5m\"\n",
		"未知のキー":          "[webhook]\nurl = \"https://h.example.com/\"\nretries = 3\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseWebhook(t, body)
			if err == nil {
				t.Fatal("起動拒否になるはずです")
			}
			if strings.Contains(err.Error(), s) {
				t.Errorf("エラー文に値が出ています: %v", err)
			}
		})
	}
}

// ループバック（127.0.0.0/8・::1）と localhost への http は許す（自分の機の受け手・テスト用）。
func TestWebhookAllowsHTTPToLoopbackOnly(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:9000/h", "http://127.0.0.2/h", "http://localhost/h", "http://[::1]:9/h"} {
		if _, err := parseWebhook(t, fmt.Sprintf("[webhook]\nurl = %q\n", u)); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
}

// Secret は fmt・slog・JSON のどれでも値を出さない（設定を丸ごとログに出しても漏れない）。
func TestSecretNeverPrintsValue(t *testing.T) {
	cfg, err := parseWebhook(t, "[webhook]\nurl = \"https://h.example.com/PATHSECRET\"\nsecret = \"KEYSECRET\"\ntoken = \"TOKSECRET\"\n")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("設定", "webhook", cfg.Webhook, "url", cfg.Webhook.URL)
	js, _ := json.Marshal(cfg.Webhook) //nolint:gosec // 値が出ないことを確かめるために、わざと書き出す
	text := strings.Join([]string{
		fmt.Sprintf("%v", cfg.Webhook), fmt.Sprintf("%+v", cfg.Webhook), fmt.Sprintf("%#v", cfg.Webhook), fmt.Sprintf("%s", any(cfg.Webhook.Token)),
		fmt.Sprint(cfg), fmt.Sprintf("%+v", *cfg), logs.String(), string(js),
	}, "\n")
	for _, secret := range []string{"PATHSECRET", "KEYSECRET", "TOKSECRET"} {
		if strings.Contains(text, secret) {
			t.Errorf("%q が出力に出ています", secret)
		}
	}
	if !strings.Contains(text, "[redacted]") {
		t.Error("伏せ字が出ていません")
	}
}
