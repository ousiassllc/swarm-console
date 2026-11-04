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
	// hostMax は token 名（上限 32 文字）のうちホスト名に使える文字数。
	// "viewer-" ＋ ホスト名 ＋ "-yyyymmdd"、さらに "-100" まで付けても 32 文字に収める。
	hostMax = 12
)

var nonNameChar = regexp.MustCompile(`[^a-z0-9_-]+`)

// tokenBase は自動の token 名 "viewer-<ホスト名>-<yyyymmdd>"。ホスト名は token 名の規則に合わせて整える。
func tokenBase(deps Deps) string {
	host, _ := deps.Hostname() // 取れないときは空のまま "host" にする
	host = strings.Trim(nonNameChar.ReplaceAllString(strings.ToLower(host), "-"), "-_")
	if len(host) > hostMax {
		host = strings.Trim(host[:hostMax], "-_")
	}
	if host == "" {
		host = "host"
	}
	return namePrefix + "-" + host + "-" + deps.Now().Format("20060102")
}

// addToken は report スコープの token を発行する。名前がぶつかったら末尾に -2・-3… を付けて空いている名前を探す
// （失効済みの名前も再利用できないため）。ぶつかりは auth.Add のロック内で判定されるので、同時に走っても重ならない。
// sessions が nil なら allowed_sessions を書かない（config.toml の sessions すべて）。
func addToken(path, base string, sessions []string, now time.Time) (name, plain string, err error) {
	for n := 1; n <= nameTries; n++ {
		name = base
		if n > 1 {
			name = base + "-" + strconv.Itoa(n)
		}
		plain, err = auth.Add(path, name, auth.ScopeReport, now, sessions...)
		if !errors.Is(err, auth.ErrNameInUse) {
			return name, plain, err
		}
	}
	return "", "", fmt.Errorf("token 名を決められません（%s から %s-%d まで使われています）。日付が変われば別の名前になります: %w", base, base, nameTries, err)
}
