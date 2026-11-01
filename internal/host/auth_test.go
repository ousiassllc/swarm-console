package host

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

var testNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.FixedZone("JST", 9*3600))

// 値はすべてダミー。実際のトークンは使わない。
func jwt(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".dummy-signature"
}

func claude(expiresAt, refreshAt string) string {
	s := `{"claudeAiOauth":{"accessToken":"dummy","refreshToken":"dummy","expiresAt":` + expiresAt
	if refreshAt != "" {
		s += `,"refreshTokenExpiresAt":` + refreshAt
	}
	return s + `,"subscriptionType":"team"},"mcpOAuth":{"x":{"expiresAt":1}}}`
}

func ms(d time.Duration) string  { return fmt.Sprint(testNow.Add(d).UnixMilli()) }
func sec(d time.Duration) string { return fmt.Sprint(testNow.Add(d).Unix()) }

func TestAuthStatus(t *testing.T) {
	const h = time.Hour
	tests := []struct {
		name, agent, body string
		status            string
		wantExp           time.Duration // ExpiresAt = now + wantExp。noExp なら nil
		noExp             bool
	}{
		{"claude 有効", "claude", claude(ms(-h), ms(30*24*h)), AuthValid, 30 * 24 * h, false},
		{"claude アクセス期限切れでも更新トークンが有効なら valid", "claude", claude(ms(-h), ms(10*24*h)), AuthValid, 10 * 24 * h, false},
		{"claude 更新トークンが 48h 以内", "claude", claude(ms(-h), ms(47*h)), AuthExpiringSoon, 47 * h, false},
		{"claude ちょうど 48h は expiring_soon", "claude", claude(ms(0), ms(48*h)), AuthExpiringSoon, 48 * h, false},
		{"claude 48h を 1 秒超えたら valid", "claude", claude(ms(0), ms(48*h+time.Second)), AuthValid, 48*h + time.Second, false},
		{"claude アクセスだけ（更新なし）で未来", "claude", claude(ms(5*24*h), ""), AuthValid, 5 * 24 * h, false},
		{"claude 更新トークン切れ", "claude", claude(ms(-48*h), ms(-h)), AuthExpired, -h, false},
		{"claude 実ファイルの形: expiresAt=0・更新は過去", "claude", claude("0", ms(-20*24*h)), AuthExpired, -20 * 24 * h, false},
		{"claude expiresAt=0・更新なし＝ログアウト", "claude", claude("0", ""), AuthExpired, 0, true},
		{"claude 両方 0", "claude", claude("0", "0"), AuthExpired, 0, true},
		{"claude expiresAt・更新とも欠落（空）", "claude", `{"claudeAiOauth":{"accessToken":"dummy"}}`, AuthExpired, 0, true},
		{"claude expiresAt・更新とも null", "claude", `{"claudeAiOauth":{"expiresAt":null,"refreshTokenExpiresAt":null}}`, AuthExpired, 0, true},
		{"claude expiresAt 欠落・更新は未来", "claude", `{"claudeAiOauth":{"refreshTokenExpiresAt":` + ms(10*24*h) + `}}`, AuthValid, 10 * 24 * h, false},
		{"claude expiresAt が文字列", "claude", `{"claudeAiOauth":{"expiresAt":"soon"}}`, AuthUnknown, 0, true},
		{"claude claudeAiOauth なし", "claude", `{"mcpOAuth":{}}`, AuthUnknown, 0, true},
		{"claude 壊れた JSON", "claude", `{"claudeAiOauth":`, AuthUnknown, 0, true},
		{"claude 空ファイル", "claude", ``, AuthUnknown, 0, true},
		{"claude 配列", "claude", `[1,2]`, AuthUnknown, 0, true},

		{"codex 有効", "codex", `{"tokens":{"access_token":"` + jwt(`{"exp":`+sec(5*24*h)+`,"email":"x@example.com"}`) + `"},"last_refresh":"2026-10-30T00:00:00Z"}`, AuthValid, 5 * 24 * h, false},
		{"codex 48h 以内", "codex", `{"tokens":{"access_token":"` + jwt(`{"exp":`+sec(2*h)+`}`) + `"}}`, AuthExpiringSoon, 2 * h, false},
		{"codex 期限切れ", "codex", `{"tokens":{"access_token":"` + jwt(`{"exp":`+sec(-h)+`}`) + `"}}`, AuthExpired, -h, false},
		{"codex exp なし", "codex", `{"tokens":{"access_token":"` + jwt(`{"sub":"x"}`) + `"}}`, AuthUnknown, 0, true},
		{"codex exp が文字列", "codex", `{"tokens":{"access_token":"` + jwt(`{"exp":"1"}`) + `"}}`, AuthUnknown, 0, true},
		{"codex JWT でない", "codex", `{"tokens":{"access_token":"opaque"}}`, AuthUnknown, 0, true},
		{"codex payload が壊れている", "codex", `{"tokens":{"access_token":"a.!!!.c"}}`, AuthUnknown, 0, true},
		{"codex tokens なし", "codex", `{"OPENAI_API_KEY":null}`, AuthUnknown, 0, true},
		{"codex access_token が数値", "codex", `{"tokens":{"access_token":5}}`, AuthUnknown, 0, true},

		{"omp expires_at 秒", "omp", `{"expires_at":` + sec(3*24*h) + `}`, AuthValid, 3 * 24 * h, false},
		{"omp expiresAt ミリ秒", "omp", `{"expiresAt":` + ms(24*h) + `}`, AuthExpiringSoon, 24 * h, false},
		{"omp exp 秒・期限切れ", "omp", `{"exp":` + sec(-h) + `}`, AuthExpired, -h, false},
		{"omp RFC 3339 文字列", "omp", `{"expires_at":"` + testNow.Add(10*24*h).UTC().Format(time.RFC3339) + `"}`, AuthValid, 10 * 24 * h, false},
		{"omp 0 は不明", "omp", `{"expires_at":0}`, AuthUnknown, 0, true},
		{"omp 該当キーなし", "omp", `{"token":"dummy"}`, AuthUnknown, 0, true},
		{"omp 配列", "omp", `[]`, AuthUnknown, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got := NewReader(clock.Real{}).AuthStatus(tt.agent, path, testNow)
			if got.Agent != tt.agent || got.Status != tt.status {
				t.Fatalf("got %+v, want status %s", got, tt.status)
			}
			if tt.noExp {
				if got.ExpiresAt != nil {
					t.Fatalf("ExpiresAt は nil のはず: %v", got.ExpiresAt)
				}
				return
			}
			if got.ExpiresAt == nil || !got.ExpiresAt.Equal(testNow.Add(tt.wantExp)) {
				t.Fatalf("ExpiresAt = %v, want now%+v", got.ExpiresAt, tt.wantExp)
			}
			if _, off := got.ExpiresAt.Zone(); off != 9*3600 {
				t.Fatalf("now と同じオフセットのはず: %v", got.ExpiresAt)
			}
		})
	}
}

func TestAuthStatusMissingAndDefaults(t *testing.T) {
	home := t.TempDir()
	r := NewReader(clock.Real{})
	r.home = home

	// 存在しない（既定の場所にもない）。
	for _, a := range []string{"claude", "codex", "omp", "other"} {
		if got := r.AuthStatus(a, "", testNow); got.Status != AuthUnknown || got.ExpiresAt != nil {
			t.Errorf("%s: unknown のはず: %+v", a, got)
		}
	}
	// 存在しない authFile・ディレクトリ。
	if got := r.AuthStatus("claude", filepath.Join(home, "nope.json"), testNow); got.Status != AuthUnknown {
		t.Errorf("存在しないファイル: %+v", got)
	}
	if got := r.AuthStatus("omp", home, testNow); got.Status != AuthUnknown {
		t.Errorf("ディレクトリ: %+v", got)
	}

	// 既定の場所。
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/.credentials.json", claude("0", ms(7*24*time.Hour)))
	write(".codex/auth.json", `{"tokens":{"access_token":"`+jwt(`{"exp":`+sec(time.Hour)+`}`)+`"}}`)
	if got := r.AuthStatus("claude", "", testNow); got.Status != AuthValid {
		t.Errorf("claude 既定: %+v", got)
	}
	if got := r.AuthStatus("codex", "", testNow); got.Status != AuthExpiringSoon {
		t.Errorf("codex 既定: %+v", got)
	}
	// `~/` は home 基準で展開。omp は設定があれば読む。
	write("omp-auth.json", `{"expires_at":`+sec(9*24*time.Hour)+`}`)
	if got := r.AuthStatus("omp", "~/omp-auth.json", testNow); got.Status != AuthValid {
		t.Errorf("omp ~ 展開: %+v", got)
	}
}

func TestAuthStatusUsesClockWhenNowZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(path, []byte(`{"exp":`+sec(time.Hour)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReader(clock.NewFake(testNow))
	if got := r.AuthStatus("omp", path, time.Time{}); got.Status != AuthExpiringSoon {
		t.Fatalf("%+v", got)
	}
}
