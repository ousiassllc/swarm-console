package claude

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

const (
	tailLines   = 12 // 作業中の行・入力欄を探す、画面の下からの行数
	askRange    = 12 // 選択肢の上下で案内文（ask_re）を探す行数
	maxActivity = 80 // 質問を活動行に出すときの最大文字数
)

var logoRE = regexp.MustCompile(`Claude Code v\d`)

// isRule は枠線の行（入力欄の上下・カードの区切り）か。
func (a *adapter) isRule(l string) bool {
	return a.P.Border.MatchString(l)
}

// inputIndex は入力欄（上下を線で挟まれた `❯` の行）の位置。なければ -1。
func (a *adapter) inputIndex(lines []string) int {
	_, base := agent.Tail(lines, tailLines)
	for i := len(lines) - 2; i >= max(base, 1); i-- {
		if a.P.Idle.MatchString(lines[i]) && a.isRule(lines[i-1]) && a.isRule(lines[i+1]) {
			return i
		}
	}
	return -1
}

// card はカーソル記号つきの選択肢と、その近くの案内文（ask_re）があれば質問カードとみなす。
func (a *adapter) card(lines []string) (agent.Choice, bool) {
	ch, ok := agent.FindChoice(lines, "❯", func(i int) bool {
		return i > 0 && i+1 < len(lines) && a.isRule(lines[i-1]) && a.isRule(lines[i+1])
	})
	if !ok {
		return ch, false
	}
	lo, hi := max(0, ch.Start-askRange), min(len(lines), ch.End+askRange)
	return ch, a.P.Ask.MatchString(strings.Join(lines[lo:hi], "\n"))
}

// workingIndex は作業中の行（esc to interrupt）の位置。なければ -1。
func (a *adapter) workingIndex(lines []string) int {
	_, base := agent.Tail(lines, tailLines)
	for i := len(lines) - 1; i >= base; i-- {
		if a.P.Working.MatchString(lines[i]) {
			return i
		}
	}
	return -1
}

// spinnerLine は画面の下のほうで、作業中の目印を含みスピナー記号で始まる行（活動行）を返す。なければ ""。
func (a *adapter) spinnerLine(lines []string) string {
	_, base := agent.Tail(lines, tailLines)
	for i := len(lines) - 1; i >= base; i-- {
		t := strings.TrimLeftFunc(lines[i], unicode.IsSpace)
		r := []rune(t)
		if len(r) > 1 && r[1] == ' ' && a.P.Spinner.MatchString(string(r[0])) && a.P.Working.MatchString(t) {
			return t
		}
	}
	return ""
}

// Detect は画面の状態を判定する。質問カード → 作業中 → 入力待ちの順に見る。
func (a *adapter) Detect(s agent.Screen) agent.Detection {
	lines := agent.TrimScreen(s.Lines)
	if ch, ok := a.card(lines); ok {
		return agent.Detection{State: agent.StateAsking, Activity: agent.Shorten(agent.QuestionAbove(lines, ch.Start), maxActivity)}
	}
	if idx := a.workingIndex(lines); idx >= 0 {
		d := agent.Detection{State: agent.StateWorking}
		if sp := a.spinnerLine(lines); sp != "" {
			d.Elapsed = agent.FindElapsed(sp)
			text := strings.TrimSpace(string([]rune(sp)[1:]))
			if i := strings.Index(text, " ("); i >= 0 {
				text = text[:i]
			}
			d.Activity = text
		}
		return d
	}
	if a.inputIndex(lines) >= 0 {
		return agent.Detection{State: agent.StateIdle}
	}
	return agent.Detection{State: agent.StateUnknown}
}
