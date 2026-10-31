package agent

import (
	"regexp"
	"strings"
	"unicode"
)

// maxScreenLines は Screen.Lines の最大行数（コンポーネント設計 §4）。
const maxScreenLines = 80

// NewScreen は行から Screen を作る（最後の 80 行まで。Width は最長行の文字数）。
func NewScreen(lines []string) Screen {
	if len(lines) > maxScreenLines {
		lines = lines[len(lines)-maxScreenLines:]
	}
	w := 0
	for _, l := range lines {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	return Screen{Lines: lines, Width: w, Height: len(lines)}
}

// sampleHeader は実画面でないサンプルの先頭行（testdata/screens の注記）。
const sampleHeader = "# 実画面"

// SampleLines はサンプル画面のテキストを行にする。先頭の「# 実画面…」注記行と末尾の空行を除く。
func SampleLines(text string) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for len(lines) > 0 && strings.HasPrefix(lines[0], sampleHeader) {
		lines = lines[1:]
	}
	return lines
}

// TrimScreen は行末の空白と末尾の空行を取り除いた行を返す。
func TrimScreen(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// TrimBlankEdges は先頭と末尾の空行を取り除く。
func TrimBlankEdges(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// CollapseBlank は連続する空行を 1 行にまとめ、先頭と末尾の空行を除く。
func CollapseBlank(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		if strings.TrimSpace(l) == "" {
			l = ""
		}
		out = append(out, l)
	}
	return TrimBlankEdges(out)
}

// Tail は末尾の n 行（足りなければ全部）と、その先頭の添字を返す。
func Tail(lines []string, n int) ([]string, int) {
	start := max(len(lines)-n, 0)
	return lines[start:], start
}

// Indent は行頭の空白の数（NBSP も 1 と数える）。
func Indent(line string) int {
	return len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
}

// BoxInner は罫線 `│ … │` の中身を返す。罫線の行でなければ ok=false。
func BoxInner(line string) (string, bool) {
	t := strings.TrimRightFunc(line, unicode.IsSpace)
	if !strings.HasPrefix(t, "│") || !strings.HasSuffix(t, "│") || len(t) < len("││") {
		return "", false
	}
	return t[len("│") : len(t)-len("│")], true
}

var (
	elapsedRE  = regexp.MustCompile(`\b\d+h(?:\s?\d+m)?\b|\b\d+m(?:\s?\d+s)?\b|\b\d+(?:\.\d+)?s\b`)
	volatileRE = regexp.MustCompile(`\b\d+h(?:\s?\d+m)?\b|\b\d+m(?:\s?\d+s)?\b|\b\d+(?:\.\d+)?(?:ms|s)\b|\b\d+:\d{2}(?::\d{2})?\b|[↑↓]?\s?\d+(?:\.\d+)?[kKM]?\s+tokens?\b`)
)

// FindElapsed は s の中の最初の経過時間表示（`36s`・`17m`・`1h05m`・`1m 5s`）を返す。なければ ""。
func FindElapsed(s string) string {
	return elapsedRE.FindString(s)
}

// StripVolatile は経過時間・時刻・トークン数を除き、空白の連続を 1 つにする（行頭の字下げは保つ）。
func StripVolatile(line string) string {
	body := volatileRE.ReplaceAllString(strings.TrimLeftFunc(line, unicode.IsSpace), "")
	return line[:len(line)-len(strings.TrimLeftFunc(line, unicode.IsSpace))] + strings.Join(strings.Fields(body), " ")
}

// StableLines は各行からスピナー・経過時間・トークン数を除いて連結する（画面ハッシュ用の本文）。
// 引数の行はすでに揮発した行（ステータス行など）を除いてあること。
func StableLines(lines []string, p *Patterns) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimRightFunc(StripVolatile(p.Spinner.ReplaceAllString(l, "")), unicode.IsSpace))
	}
	return strings.Join(TrimBlankEdges(out), "\n")
}

// Shorten は s を最大 n 文字にし、超えたぶんは「…」にする。
func Shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
