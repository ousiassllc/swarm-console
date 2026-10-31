package claude

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// TODO 表示（`⎿  ☐ task`・`☒ task`）の行。
var todoRE = regexp.MustCompile(`^\s*(?:⎿\s+)?[☐☒◼◻]\s`)

// Clean は枠線・入力欄・ステータス行・作業中の行・TODO 表示を除く。
func (a *adapter) Clean(lines []string) []string {
	ls := agent.TrimScreen(lines)
	input := a.inputIndex(ls)
	out := make([]string, 0, len(ls))
	for i, l := range ls {
		switch {
		case i == input, a.P.Border.MatchString(l), a.P.StatusLine.MatchString(l),
			a.P.Working.MatchString(l), todoRE.MatchString(l):
		default:
			if inner, ok := agent.BoxInner(l); ok {
				l = strings.TrimRightFunc(strings.TrimPrefix(inner, " "), unicode.IsSpace)
			}
			out = append(out, l)
		}
	}
	return agent.CollapseBlank(out)
}

// StableText は作業中の行（スピナー・動詞・経過・トークン数）とステータス行を除き、経過時間などを除いた本文を返す。
func (a *adapter) StableText(lines []string) string {
	ls := agent.TrimScreen(lines)
	body := make([]string, 0, len(ls))
	for _, l := range ls {
		if a.P.Working.MatchString(l) || a.P.StatusLine.MatchString(l) {
			continue
		}
		body = append(body, l)
	}
	return agent.StableLines(body, a.P)
}
