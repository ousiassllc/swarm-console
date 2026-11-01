package selfupdate

import (
	"regexp"
	"strings"
)

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// parseSums は SHA256SUMS から asset の SHA-256 を取り出す（FR-UPD-14）。
// 1 行は `sha256sum` の出力（`<64 桁の小文字 16 進>␣␣<ファイル名>`）。行末の CR（CRLF）と、
// ファイル名直前の `*`（`sha256sum -b` の形。`<ハッシュ>␣*<ファイル名>`）は許す。
// 区切りの空白の数・タブ・ハッシュの大文字は許さない。
// asset を指す行がちょうど 1 行あるときだけ ok。ほかのファイルの行は読まない。
func parseSums(text, asset string) (hash string, ok bool) {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasSuffix(line, asset) && refersTo(line, asset) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		return "", false
	}
	hash, name, found := strings.Cut(lines[0], " ")
	if !found || !hashRe.MatchString(hash) {
		return "", false
	}
	// 区切りは空白 1 つ（text モード）か `*`（binary モード）が続く。
	if name != " "+asset && name != "*"+asset {
		return "", false
	}
	return hash, true
}

// refersTo は line の末尾の asset が、ファイル名として区切られている（前が行頭・空白・`*`）かを返す。
func refersTo(line, asset string) bool {
	rest := strings.TrimSuffix(line, asset)
	if rest == "" {
		return true
	}
	last := rest[len(rest)-1]
	return last == ' ' || last == '*' || last == '\t'
}
