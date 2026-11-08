package pair

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

const (
	namePrefix = "viewer"
	// nameTries は自動の token 名がぶつかったときに、末尾へ -2・-3… を付けて試す上限（1 回目は付けない）。
	nameTries = 100
	// nameMax は token 名の上限（auth.ValidateName）。名前の残り（ホスト名の長さ）は、これから先頭・日付・"-100" を引いて決める。
	nameMax = 32
	// nameSuffix は "-yyyymmdd"（9 文字）と、ぶつかったときに付く "-100"（4 文字）。
	nameSuffix = 9 + 4
)

var nonNameChar = regexp.MustCompile(`[^a-z0-9_-]+`)

// namePrefixOf は token 名の先頭。report は "viewer"、ops は区別できるように "viewer-ops"。
func namePrefixOf(scope auth.Scope) string {
	if scope == auth.ScopeOps {
		return namePrefix + "-" + string(auth.ScopeOps)
	}
	return namePrefix
}

// tokenBase は自動の token 名 "viewer-<ホスト名>-<yyyymmdd>"（ops は "viewer-ops-<ホスト名>-<yyyymmdd>"）。
// ホスト名は token 名の規則に合わせて整える。末尾に "-100" まで付けても 32 文字に収める。
func tokenBase(deps Deps, scope auth.Scope) string {
	prefix := namePrefixOf(scope)
	hostMax := nameMax - len(prefix) - 1 - nameSuffix
	host, _ := deps.Hostname() // 取れないときは空のまま "host" にする
	host = strings.Trim(nonNameChar.ReplaceAllString(strings.ToLower(host), "-"), "-_")
	if len(host) > hostMax {
		host = strings.Trim(host[:hostMax], "-_")
	}
	if host == "" {
		host = "host"
	}
	return prefix + "-" + host + "-" + deps.Now().Format("20060102")
}

// addToken は scope の token を発行する。名前がぶつかったら末尾に -2・-3… を付けて空いている名前を探す
// （失効済みの名前も再利用できないため）。ぶつかりは auth.Add のロック内で判定されるので、同時に走っても重ならない。
// sessions が nil なら allowed_sessions を書かない（config.toml の sessions すべて）。
func addToken(path, base string, scope auth.Scope, sessions []string, now time.Time) (name, plain string, err error) {
	for n := 1; n <= nameTries; n++ {
		name = base
		if n > 1 {
			name = base + "-" + strconv.Itoa(n)
		}
		plain, err = auth.Add(path, name, scope, now, sessions...)
		if !errors.Is(err, auth.ErrNameInUse) {
			return name, plain, err
		}
	}
	return "", "", fmt.Errorf("token 名を決められません（%s から %s-%d まで使われています）。日付が変われば別の名前になります: %w", base, base, nameTries, err)
}
