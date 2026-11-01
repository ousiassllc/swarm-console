package omp

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

var (
	todoBoxRE  = regexp.MustCompile(`^╭─+\s*\p{Co}?\s*Todo \d+ tasks?\b`)
	todoTreeRE = regexp.MustCompile(`^\s*TODO\s*$`)
	todoRowRE  = regexp.MustCompile(`^\s+[├└│]`)
)

// chromeStart は画面下の飾り（活動行・タイトル行・ステータス行・入力欄の下枠）の先頭の位置。
// ステータス行が見えないときは len(lines)。withActivity が false なら活動行・タイトル行は飾りに含めない。
func (a *adapter) chromeStart(lines []string, withActivity bool) int {
	idx := a.statusIndex(lines)
	if idx < 0 {
		return len(lines)
	}
	if withActivity && idx > 0 {
		t := []rune(strings.TrimLeftFunc(lines[idx-1], unicode.IsSpace))
		if len(t) > 0 && unicode.Is(unicode.Co, t[0]) {
			return idx - 1
		}
	}
	return idx
}

// Clean は枠線・TODO 表示・ステータス行（活動行・タイトル行を含む）を除き、枠の中身は罫線を外して返す。
func (a *adapter) Clean(lines []string) []string {
	ls := agent.TrimScreen(lines)
	ls = ls[:a.chromeStart(ls, true)]
	out := make([]string, 0, len(ls))
	for i := 0; i < len(ls); i++ {
		l := ls[i]
		switch {
		case todoBoxRE.MatchString(l):
			for i < len(ls) && !strings.HasPrefix(ls[i], "╰") {
				i++
			}
		case todoTreeRE.MatchString(l):
			for i+1 < len(ls) && todoRowRE.MatchString(ls[i+1]) {
				i++
			}
		case a.P.Border.MatchString(l):
		default:
			if inner, ok := agent.BoxInner(l); ok {
				l = strings.TrimRightFunc(strings.TrimPrefix(inner, " "), unicode.IsSpace)
			}
			out = append(out, l)
		}
	}
	return agent.CollapseBlank(out)
}

// StableText はステータス行（スピナー・経過・トークン数）を除き、本文の経過時間表示も除いた文字列を返す。
func (a *adapter) StableText(lines []string) string {
	ls := agent.TrimScreen(lines)
	return agent.StableLines(ls[:a.chromeStart(ls, false)], a.P)
}
