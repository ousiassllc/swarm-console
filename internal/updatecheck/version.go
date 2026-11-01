// Package updatecheck は GitHub Releases の最新版の取得・版の比較・結果の保持・serve の定期確認を担う（FR-UPD-1〜10）。
package updatecheck

import (
	"regexp"

	"golang.org/x/mod/semver"
)

// tagRe はリリースの版の形（FR-UPD-3。Issue #8 が付けるタグと同じ）。
var tagRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ValidTag は s が vX.Y.Z の形かを返す。GitHub が返した文字列を応答・表示・URL に使う前に必ず通す（NFR-SEC-15）。
func ValidTag(s string) bool { return tagRe.MatchString(s) }

// ValidCurrent は動いているバイナリの版が比較できる形かを返す。
// vX.Y.Z か、プレリリース形（v1.2.0-rc.1）。`dev`・`v1.2`・ビルドメタデータ付きは false（FR-UPD-3）。
func ValidCurrent(s string) bool {
	return semver.IsValid(s) && semver.Canonical(s) == s
}

// Newer は a が b より semver で新しいかを返す。どちらかが semver として読めなければ false。
func Newer(a, b string) bool {
	return semver.IsValid(a) && semver.IsValid(b) && semver.Compare(a, b) > 0
}

// ReleaseURL は版 tag（ValidTag を通ったもの）のリリースページの URL。
func ReleaseURL(tag string) string {
	return "https://github.com/" + Owner + "/" + Repo + "/releases/tag/" + tag
}
