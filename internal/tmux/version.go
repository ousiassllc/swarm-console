package tmux

import (
	"regexp"
	"strconv"
	"strings"
)

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// CheckVersion は `tmux -V` の出力（"tmux 3.2a"・"tmux next-3.5"・"tmux master" など）から、
// 3.x 前提と load-buffer -（標準入力）・paste-buffer -p（2.x 以降）が使えるかを判定する（P-24）。
// 問題なければ ""、足りなければ警告文を返す。
func CheckVersion(v string) (warning string) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return "tmux のバージョンを取得できませんでした（3.x 以降を前提としています）"
	}
	tok := f[len(f)-1]
	if tok == "master" || strings.HasPrefix(tok, "next-") && !versionRe.MatchString(tok) {
		return "" // 開発版は最新扱い
	}
	m := versionRe.FindStringSubmatch(tok)
	if m == nil {
		return "tmux のバージョン「" + tok + "」を判定できません（3.x 以降を前提としています）"
	}
	major, _ := strconv.Atoi(m[1])
	switch {
	case major < 2:
		return "tmux " + m[1] + "." + m[2] + " は paste-buffer -p（bracketed paste）に対応していません。3.x 以降が必要です"
	case major < 3:
		return "tmux " + m[1] + "." + m[2] + " は 3.x 前提の動作確認をしていません。3.x 以降を推奨します"
	}
	return ""
}
