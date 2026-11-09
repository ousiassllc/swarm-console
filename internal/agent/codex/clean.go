package codex

import (
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Clean は起動枠（`╭…╰`）・起動の見出し行（`>_ OpenAI Codex (v…)`）・入力欄（画面で最後の `› `）・ステータス行・作業中の行を除く。
func (a *adapter) Clean(lines []string) []string {
	ls := agent.TrimScreen(lines)
	out := make([]string, 0, len(ls))
	inBox := false
	input := a.inputIndex(ls)
	for i, l := range ls {
		switch {
		case strings.HasPrefix(strings.TrimSpace(l), "╭"):
			inBox = true
		case strings.HasPrefix(strings.TrimSpace(l), "╰"):
			inBox = false
		case inBox, i == input, bannerRE.MatchString(l), a.P.Border.MatchString(l), a.P.StatusLine.MatchString(l), a.P.Working.MatchString(l):
		default:
			out = append(out, l)
		}
	}
	return agent.CollapseBlank(out)
}

// StableText は作業中の行・ステータス行（残量）を除き、経過時間などを除いた本文を返す。
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
