package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	paneNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	// 自動の位置名（FR-POS-2）。T1・B2・R1 段の 2 列目（R12）など。大文字小文字は区別しない。
	autoNameRe = regexp.MustCompile(`(?i)^(?:[TB]|R[0-9]+)[0-9]+$`)
	paneKeyRe  = regexp.MustCompile(`^(.+):([0-9]+)\.([0-9]+)$`)
)

// checkPaneNames は [pane_names] を検査する。キーは "<セッション>:<ウィンドウ>.<ペイン番号>"（セッションは sessions のどれか）、
// 名前は paneNameRe で、自動名（T1 など）・他の固定名（大文字小文字を区別しない）と衝突しない。
func checkPaneNames(in map[string]string, sessions []string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	known := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		known[s] = true
	}
	byName := make(map[string]string, len(in))
	out := make(map[string]string, len(in))
	for key, name := range in {
		m := paneKeyRe.FindStringSubmatch(key)
		if m == nil || !known[m[1]] {
			return nil, fmt.Errorf("pane_names のキー %q は <セッション>:<ウィンドウ>.<ペイン番号> の形で、セッションは sessions のどれかにしてください", key)
		}
		if !paneNameRe.MatchString(name) {
			return nil, fmt.Errorf("pane_names[%q] の名前は英字で始まる英数字・_・- にしてください", key)
		}
		if autoNameRe.MatchString(name) {
			return nil, fmt.Errorf("pane_names[%q] の名前は自動の位置名（T1・B2 など）と衝突します", key)
		}
		lower := strings.ToLower(name)
		if other, dup := byName[lower]; dup {
			a, b := key, other
			if b < a {
				a, b = b, a
			}
			return nil, errors.New("pane_names の名前が衝突しています: " + a + " と " + b)
		}
		byName[lower] = key
		out[key] = name
	}
	return out, nil
}
