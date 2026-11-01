package host

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	soonWindow   = 48 * time.Hour
	maxAuthBytes = 1 << 20 // 認証ファイルはこれ以上読まない
)

// expiry は認証ファイルから読めた期限。
type expiry struct {
	at      time.Time // 期限。cleared のときは零値
	cleared bool      // 期限の日時はないが「失効済み」と読めた（Claude Code のログアウト状態）
}

// parser は認証ファイルの中身から期限だけを取り出す。読めなければ ok=false。
// トークン文字列・メールなど期限以外は読み取っても保持・返却しない。
type parser func(data []byte) (e expiry, ok bool)

// AuthStatus は agent の認証期限を読む。authFile が空なら既定の場所（P-16、docs/architecture/agent-auth.md）。
// ファイルが無い・読めない・形式が違うときは AuthUnknown（エラーにしない）。期限以外の中身は返さない。
func (r *Reader) AuthStatus(agent, authFile string, now time.Time) Auth {
	unknown := Auth{Agent: agent, Status: AuthUnknown}
	if now.IsZero() {
		now = r.clock.Now()
	}
	p, path := parserFor(agent), r.authPath(agent, authFile)
	if p == nil || path == "" {
		return unknown
	}
	data, err := readLimited(path)
	if err != nil {
		return unknown
	}
	e, ok := p(data)
	if !ok {
		return unknown
	}
	if e.cleared {
		return Auth{Agent: agent, Status: AuthExpired}
	}
	at := e.at.In(now.Location())
	status := AuthValid
	switch d := at.Sub(now); {
	case d <= 0:
		status = AuthExpired
	case d <= soonWindow:
		status = AuthExpiringSoon
	}
	return Auth{Agent: agent, Status: status, ExpiresAt: &at}
}

func parserFor(agent string) parser {
	switch agent {
	case "claude":
		return parseClaude
	case "codex":
		return parseCodex
	case "omp":
		return parseGeneric
	}
	return nil
}

// authPath は設定の authFile（`~` 展開）か、エージェントごとの既定の場所。omp に既定はない。
func (r *Reader) authPath(agent, authFile string) string {
	home := r.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if authFile != "" {
		if rest, ok := strings.CutPrefix(authFile, "~/"); ok && home != "" {
			return filepath.Join(home, rest)
		}
		return authFile
	}
	if home == "" {
		return ""
	}
	switch agent {
	case "claude":
		return filepath.Join(home, ".claude", ".credentials.json")
	case "codex":
		return filepath.Join(home, ".codex", "auth.json")
	}
	return ""
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // 設定または既定の認証ファイルのパス。読むのは期限だけ
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxAuthBytes))
}

// parseClaude は ~/.claude/.credentials.json の claudeAiOauth を読む。
//
//	{"claudeAiOauth": {"expiresAt": <epoch ミリ秒>, "refreshTokenExpiresAt": <epoch ミリ秒>, ...}}
//
// アクセストークンは数時間で切れ、リフレッシュトークンで自動更新されるため、ログインとして
// 有効かどうかの目安は refreshTokenExpiresAt（> 0 のときは expiresAt との遅いほう）。
// expiresAt・refreshTokenExpiresAt が 0・null・欠落のものだけ（有効な値が 1 つも無い）なら、
// トークンが消された（ログアウト）状態として失効扱い（P-16）。
// claudeAiOauth が無い（CLAUDE_CODE_OAUTH_TOKEN の長期トークンはここに入らない）・数値でないときは不明。
func parseClaude(data []byte) (expiry, bool) {
	var f struct {
		OAuth *struct {
			ExpiresAt        *float64 `json:"expiresAt"`
			RefreshExpiresAt *float64 `json:"refreshTokenExpiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &f) != nil || f.OAuth == nil {
		return expiry{}, false
	}
	var ms float64
	for _, v := range []*float64{f.OAuth.ExpiresAt, f.OAuth.RefreshExpiresAt} {
		if v != nil && *v > ms {
			ms = *v
		}
	}
	if ms <= 0 {
		return expiry{cleared: true}, true
	}
	return expiry{at: time.UnixMilli(int64(ms))}, true
}

// parseCodex は ~/.codex/auth.json の tokens.access_token（JWT）の payload から exp（epoch 秒）だけを読む。
// last_refresh は読まない（期限は exp が正。docs/architecture/agent-auth.md）。JWT でない・exp が無いときは不明。
func parseCodex(data []byte) (expiry, bool) {
	var f struct {
		Tokens *struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &f) != nil || f.Tokens == nil {
		return expiry{}, false
	}
	parts := strings.Split(f.Tokens.AccessToken, ".")
	if len(parts) != 3 {
		return expiry{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return expiry{}, false
	}
	var c struct {
		Exp *float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &c) != nil || c.Exp == nil || *c.Exp <= 0 {
		return expiry{}, false
	}
	return expiry{at: time.Unix(int64(*c.Exp), 0)}, true
}

// parseGeneric は設定の auth_file（omp など）の最上位の expires_at・expiresAt・exp のいずれかを読む。
// 数値は 1e12 以上なら epoch ミリ秒、未満なら秒。文字列は RFC 3339。0 以下・それ以外は不明。
func parseGeneric(data []byte) (expiry, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return expiry{}, false
	}
	for _, key := range []string{"expires_at", "expiresAt", "exp"} {
		raw, ok := m[key]
		if !ok {
			continue
		}
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			if n <= 0 {
				return expiry{}, false
			}
			if n >= 1e12 {
				return expiry{at: time.UnixMilli(int64(n))}, true
			}
			return expiry{at: time.Unix(int64(n), 0)}, true
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return expiry{at: t}, true
			}
		}
		return expiry{}, false
	}
	return expiry{}, false
}
