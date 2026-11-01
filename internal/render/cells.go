package render

import (
	"strings"
	"unicode/utf8"
)

const (
	emptyCell   = "—"
	maxCellRune = 80
	ellipsis    = "…"
)

// flatten は制御文字（改行・タブ含む）を空白 1 つにし、不正な UTF-8 を置き換える。
func flatten(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// truncate は s を n 文字（rune）までにする。超えたら先頭 n-1 文字＋「…」（全体で n 文字）。
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + ellipsis
}

// cell は表のセル用に整える。空は「—」、長いものは 80 字で「…」、`|` はエスケープ。
func cell(s string) string {
	s = strings.TrimSpace(flatten(s))
	if s == "" {
		return emptyCell
	}
	s = truncate(s, maxCellRune)
	return strings.ReplaceAll(s, "|", `\|`)
}

// line は箇条書き 1 行用。改行などを空白にして前後を落とす。空は「—」。切り詰めはしない。
func line(s string) string {
	s = strings.TrimSpace(flatten(s))
	if s == "" {
		return emptyCell
	}
	return s
}

// block は複数行の値を箇条書きの項目に収める。改行は 2 字下げの続き行にする（\r\n・\r は \n に正規化）。
func block(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return emptyCell
	}
	return strings.ReplaceAll(s, "\n", "\n  ")
}

// fence は lines をコードブロックにする。本文中のバッククォート列より長い囲みを使う。
func fence(lines []string) string {
	body := strings.ToValidUTF8(strings.Join(lines, "\n"), "\uFFFD")
	body = strings.ReplaceAll(body, "\r", "")
	longest, run := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	f := strings.Repeat("`", max(3, longest+1))
	return f + "\n" + body + "\n" + f
}
