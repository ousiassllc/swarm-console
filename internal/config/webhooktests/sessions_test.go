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

func parse(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	return config.ParseBytes([]byte(`listen_addr = "100.64.1.2:48120"`+"\n"+body), "config.toml")
}

// Issue #95: [webhook] だけの書き方（#93 の形）は、sessions の表が無くてもそのまま動く。全セッションが [webhook] の送り先を使う。
func TestWebhookOnlyConfigKeepsWorkingForEverySession(t *testing.T) {
	cfg, err := parse(t, "sessions = [\"0\", \"1\"]\n[webhook]\nurl = \"https://hooks.example.com/t/abc\"\nsecret = \"k\"\ntoken = \"tok\"\nmax_attempts = 3\n")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Webhook.Enabled() || len(cfg.Webhook.Sessions) != 0 {
		t.Fatalf("webhook = %+v", cfg.Webhook)
	}
	for _, s := range []string{"0", "1", "2"} {
		w, ok := cfg.Webhook.Target(s)
		if !ok || w.URL.Reveal() != "https://hooks.example.com/t/abc" || w.Secret.Reveal() != "k" || w.Token.Reveal() != "tok" || w.MaxAttempts != 3 {
			t.Errorf("セッション %s の送り先 = %+v ok=%v", s, w, ok)
		}
	}
}

// セッションの表があるセッションはその送り先、無いセッションは [webhook]。url・secret・token は受け継がず、再送の設定は書かなければ受け継ぐ。
func TestWebhookSessionTablesOverrideAndInheritRetrySettings(t *testing.T) {
	cfg, err := parse(t, `sessions = ["0", "1", "2"]
[webhook]
url = "https://default.example.com/h"
secret = "default-secret"
token = "default-token"
max_attempts = 4
retry_interval = "2s"
timeout = "20s"

[webhook.sessions."1"]
url = "https://one.example.com/h"
max_attempts = 2

[webhook.sessions."2"]
url = "https://two.example.com/h"
secret = "two-secret"
token = "two-token"
retry_interval = "5s"
timeout = "3s"
`)
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Webhook
	if got, ok := w.Target("0"); !ok || got.URL.Reveal() != "https://default.example.com/h" || got.Secret.Reveal() != "default-secret" {
		t.Errorf("セッション 0（表なし）は [webhook] を使うはず: %+v ok=%v", got, ok)
	}
	one, ok := w.Target("1")
	if !ok || one.URL.Reveal() != "https://one.example.com/h" || one.Secret != "" || one.Token != "" {
		t.Errorf("セッション 1 は自分の URL で、[webhook] の secret・token を受け継がないはず: %+v", one)
	}
	if one.MaxAttempts != 2 || one.RetryInterval != 2*time.Second || one.Timeout != 20*time.Second {
		t.Errorf("セッション 1 の再送の設定 = %d・%s・%s", one.MaxAttempts, one.RetryInterval, one.Timeout)
	}
	two, ok := w.Target("2")
	if !ok || two.Secret.Reveal() != "two-secret" || two.Token.Reveal() != "two-token" ||
		two.MaxAttempts != 4 || two.RetryInterval != 5*time.Second || two.Timeout != 3*time.Second {
		t.Errorf("セッション 2 = %+v ok=%v", two, ok)
	}
}

// [webhook] の url が無くても、セッションの表に url があれば有効。表の無いセッションは送り先なし。
func TestWebhookSessionsWithoutDefaultURL(t *testing.T) {
	cfg, err := parse(t, "sessions = [\"0\", \"1\"]\n[webhook.sessions.\"1\"]\nurl = \"https://one.example.com/h\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Webhook.Enabled() {
		t.Error("セッションの表だけでも有効になるはずです")
	}
	if _, ok := cfg.Webhook.Target("0"); ok {
		t.Error("表の無いセッション 0 に送り先があります")
	}
	if w, ok := cfg.Webhook.Target("1"); !ok || w.MaxAttempts != 5 {
		t.Errorf("セッション 1 = %+v ok=%v", w, ok)
	}
}

// セッションの表の url が空なら、そのセッションだけオフ（[webhook] があっても使わない）。全部空ならオフ。
func TestWebhookSessionEmptyURLOptsOut(t *testing.T) {
	cfg, err := parse(t, "sessions = [\"0\", \"1\"]\n[webhook]\nurl = \"https://d.example.com/h\"\n[webhook.sessions.\"0\"]\nurl = \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Webhook.Target("0"); ok {
		t.Error("url が空のセッション 0 に送り先があります")
	}
	if _, ok := cfg.Webhook.Target("1"); !ok {
		t.Error("セッション 1 は [webhook] を使うはずです")
	}
	cfg, err = parse(t, "[webhook.sessions.\"0\"]\nurl = \"\"\n")
	if err != nil || cfg.Webhook.Enabled() {
		t.Errorf("url が全部空ならオフ: enabled=%v err=%v", cfg.Webhook.Enabled(), err)
	}
}

// sessions にない名前の表は起動拒否。エラー文に値（URL・鍵・トークン）は入らず、名前は入る。
func TestWebhookSessionTableOutsideSessionsIsRejected(t *testing.T) {
	const s = "SECRETVALUE-88"
	_, err := parse(t, fmt.Sprintf("sessions = [\"0\", \"1\"]\n[webhook.sessions.\"7\"]\nurl = \"https://h.example.com/%s\"\nsecret = %q\ntoken = %q\n", s, s, s))
	if err == nil {
		t.Fatal("起動拒否になるはずです")
	}
	if strings.Contains(err.Error(), s) || !strings.Contains(err.Error(), `"7"`) {
		t.Errorf("エラー文: %v", err)
	}
	// 既定の sessions（"0" だけ）に対して "1" を書いても同じ。
	if _, err := parse(t, "[webhook.sessions.\"1\"]\nurl = \"https://h.example.com/\"\n"); err == nil {
		t.Error("sessions に無い \"1\" を受け付けました")
	}
}

// セッションの表の不正な値は起動拒否。エラー文にキー（webhook.sessions."1".…）は入り、値は入らない。
func TestWebhookSessionTableRejectsBadValuesWithoutEchoingSecrets(t *testing.T) {
	const s = "SECRETVALUE-99"
	head := "sessions = [\"0\", \"1\"]\n[webhook.sessions.\"1\"]\n"
	for name, c := range map[string]struct{ body, key string }{
		"http で外へ":       {fmt.Sprintf("url = \"http://hooks.example.com/%s\"\n", s), `webhook.sessions."1".url`},
		"ユーザー情報":         {fmt.Sprintf("url = \"https://user:%s@hooks.example.com/\"\n", s), `webhook.sessions."1".url`},
		"token に改行":      {fmt.Sprintf("url = \"https://h.example.com/\"\ntoken = \"a\\n%s\"\n", s), `webhook.sessions."1".token`},
		"secret に制御文字":   {fmt.Sprintf("url = \"https://h.example.com/\"\nsecret = \"a\\u0007%s\"\n", s), `webhook.sessions."1".secret`},
		"max_attempts 0": {"url = \"https://h.example.com/\"\nmax_attempts = 0\n", `webhook.sessions."1".max_attempts`},
		"retry 短い":       {"url = \"https://h.example.com/\"\nretry_interval = \"10ms\"\n", `webhook.sessions."1".retry_interval`},
		"timeout 長い":     {"url = \"https://h.example.com/\"\ntimeout = \"5m\"\n", `webhook.sessions."1".timeout`},
		"未知のキー":          {"url = \"https://h.example.com/\"\nretries = 3\n", "retries"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, head+c.body)
			if err == nil {
				t.Fatal("起動拒否になるはずです")
			}
			if strings.Contains(err.Error(), s) || !strings.Contains(err.Error(), c.key) {
				t.Errorf("エラー文: %v", err)
			}
		})
	}
}

// セッションごとの値（URL・鍵・トークン）も、fmt・slog・JSON のどれでも値を出さない。
func TestSessionSecretsNeverPrintValue(t *testing.T) {
	cfg, err := parse(t, "sessions = [\"0\", \"1\"]\n[webhook.sessions.\"1\"]\nurl = \"https://h.example.com/PATHSECRET\"\nsecret = \"KEYSECRET\"\ntoken = \"TOKSECRET\"\n")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("設定", "webhook", cfg.Webhook)
	js, _ := json.Marshal(cfg.Webhook) //nolint:gosec // 値が出ないことを確かめるために、わざと書き出す
	text := strings.Join([]string{
		fmt.Sprintf("%v", cfg.Webhook), fmt.Sprintf("%+v", cfg.Webhook), fmt.Sprintf("%#v", cfg.Webhook),
		fmt.Sprint(cfg), fmt.Sprintf("%+v", *cfg), logs.String(), string(js),
	}, "\n")
	for _, secret := range []string{"PATHSECRET", "KEYSECRET", "TOKSECRET"} {
		if strings.Contains(text, secret) {
			t.Errorf("%q が出力に出ています", secret)
		}
	}
}
