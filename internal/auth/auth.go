// Package auth は tokens.toml（ER §3）の読み書きと Bearer トークンの認証を行う。
// トークンの平文・ハッシュ・Authorization ヘッダーはログにもエラーにも出さない（NFR-SEC-5）。
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

// Scope はトークンが使える窓口。
type Scope string

// 窓口の一覧。ops は report を含む（P-03）。
const (
	ScopeReport Scope = "report"
	ScopeOps    Scope = "ops"
)

// Client は認証に成功したクライアント。
type Client struct {
	Name  string
	Scope Scope
}

type clientKey struct{}

// WithClient は c を載せた ctx を返す。
func WithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

// FromContext は ctx に載っているクライアントを返す。無ければ false。
func FromContext(ctx context.Context) (Client, bool) {
	c, ok := ctx.Value(clientKey{}).(Client)
	return c, ok
}

// Allowed は c が endpoint（"/report" か "/ops"）を使えるかを返す。
// /ops は ops だけ（違えば forbidden_scope）。/report は report・ops どちらも通る。
func Allowed(c Client, endpoint string) error {
	switch endpoint {
	case "/report":
		if c.Scope == ScopeReport || c.Scope == ScopeOps {
			return nil
		}
		return apperr.New(apperr.ForbiddenScope)
	case "/ops":
		if c.Scope == ScopeOps {
			return nil
		}
		return apperr.New(apperr.ForbiddenScope)
	default:
		return apperr.New(apperr.NotFound)
	}
}

// entry は照合用に読み込んだトークン 1 件。hash は "sha256:<hex>" の文字列そのもの。
type entry struct {
	name    string
	scope   Scope
	hash    []byte
	revoked bool
}

// Authenticator は tokens.toml を持ち、呼び出しごとに更新を検知して認証する。
type Authenticator struct {
	path    string
	clk     clock.Clock
	compare func(a, b []byte) int // 比較関数。テストで呼び出しを数えるために差し替えられる

	mu      sync.RWMutex
	entries []entry
	stamp   fileStamp
	healthy bool // 直近の読み込みが成功したか。false の間は認証を通さない
}

// Open は path の tokens.toml を読んで Authenticator を返す。
// 権限が 600 でない・TOML が壊れている・未知の scope・name/hash 形式違反は error（起動拒否）。
// ファイルが無ければ空で始め、後からできたら次の Authenticate で読み込む。
func Open(path string, clk clock.Clock) (*Authenticator, error) {
	a := &Authenticator{path: path, clk: clk, compare: subtle.ConstantTimeCompare}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.load(statFile(path)); err != nil {
		return nil, err
	}
	return a, nil
}

// Authenticate は "Bearer <token>" を検証してクライアントを返す。
// 失敗は理由を問わず同一の apperr.Unauthorized（なし・形式違い・不一致・失効・ファイル不良）。
func (a *Authenticator) Authenticate(authorization string) (Client, error) {
	token, ok := bearerToken(authorization)
	a.refresh()
	if !ok {
		return Client{}, apperr.New(apperr.Unauthorized)
	}
	sum := sha256.Sum256([]byte(token))
	want := []byte("sha256:" + hex.EncodeToString(sum[:]))

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.healthy {
		return Client{}, apperr.New(apperr.Unauthorized)
	}
	// 全エントリを必ず走査する（早期終了しない。失効済みも比較する）。
	found := -1
	for i := range a.entries {
		if a.compare(a.entries[i].hash, want) == 1 && !a.entries[i].revoked {
			found = i
		}
	}
	if found < 0 {
		return Client{}, apperr.New(apperr.Unauthorized)
	}
	e := a.entries[found]
	return Client{Name: e.name, Scope: e.scope}, nil
}

// bearerToken は "Bearer <token>"（スキーム名の大文字小文字は不問）から token を取り出す。
func bearerToken(authorization string) (string, bool) {
	scheme, token, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// refresh はファイルの状態（mtime・size・権限）が変わっていたら再読み込みする。
func (a *Authenticator) refresh() {
	st := statFile(a.path)
	a.mu.RLock()
	same := !st.failed && st == a.stamp
	a.mu.RUnlock()
	if same {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !st.failed && st == a.stamp {
		return // 別の goroutine が先に読み込んだ
	}
	if err := a.load(st); err != nil {
		// 詳細（パス・権限・構文）だけを出す。トークン・ハッシュは含まれない。
		slog.Warn("tokens.toml を読み込めないため認証を拒否します", "error", err)
	}
}

// load は a.mu を書き込みロックした状態で呼ぶ。失敗時は認証を通さない状態にする。
func (a *Authenticator) load(st fileStamp) error {
	a.stamp = st
	recs, err := readRecords(a.path)
	if err != nil {
		a.entries, a.healthy = nil, false
		return err
	}
	entries := make([]entry, len(recs))
	for i, r := range recs {
		entries[i] = entry{name: r.Name, scope: Scope(r.Scope), hash: []byte(r.Hash), revoked: r.RevokedAt != ""}
	}
	a.entries, a.healthy = entries, true
	return nil
}

// fileStamp は再読み込みの要否を決めるファイルの状態。権限変更（mtime は変わらない）も拾う。
type fileStamp struct {
	exists  bool
	modNano int64
	size    int64
	mode    os.FileMode
	failed  bool // stat が NotExist 以外で失敗した。毎回再読み込みを試みる
}

func statFile(path string) fileStamp {
	fi, err := os.Stat(path)
	switch {
	case err == nil:
		return fileStamp{exists: true, modNano: fi.ModTime().UnixNano(), size: fi.Size(), mode: fi.Mode().Perm()}
	case os.IsNotExist(err):
		return fileStamp{}
	default:
		return fileStamp{failed: true}
	}
}
