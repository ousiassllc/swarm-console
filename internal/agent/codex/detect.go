package codex

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

var (
	bannerRE   = regexp.MustCompile(`>_ OpenAI Codex`)
	numberedRE = regexp.MustCompile(`^\s*›\s+\d+\.\s`)
)

// card は番号つきでカーソル記号（`›`）のある選択肢と、その近くの案内文（ask_re）があれば承認カードとみなす。
// 番号のない `› Ask Codex to do anything` の入力欄は選択肢にしない。
func (a *adapter) card(lines []string) (agent.Choice, bool) {
	ch, ok := agent.FindChoice(lines, "›", nil)
	if !ok || !ch.Numbered {
		return ch, false
	}
	lo, hi := max(0, ch.Start-askRange), min(len(lines), ch.End+askRange)
	return ch, a.P.Ask.MatchString(strings.Join(lines[lo:hi], "\n"))
}

// workingLine は画面の下のほうで作業中の目印（esc to interrupt）を含む行を返す。なければ ok=false。
func (a *adapter) workingLine(lines []string) (string, bool) {
	_, base := agent.Tail(lines, tailLines)
	for i := len(lines) - 1; i >= base; i-- {
		if a.P.Working.MatchString(lines[i]) {
			return strings.TrimLeftFunc(lines[i], unicode.IsSpace), true
		}
	}
	return "", false
}

// inputIndex は画面の下のほうにある入力欄（最後の `› ` の行）の位置。なければ -1。
// 番号つきの承認カードの選択肢（`› 1. Yes`）は入力欄ではない。
func (a *adapter) inputIndex(lines []string) int {
	_, base := agent.Tail(lines, tailLines)
	for i := len(lines) - 1; i >= base; i-- {
		if a.P.Idle.MatchString(lines[i]) && !numberedRE.MatchString(lines[i]) {
			return i
		}
	}
	return -1
}

// Detect は画面の状態を判定する。承認カード → 作業中 → 入力待ちの順に見る。
func (a *adapter) Detect(s agent.Screen) agent.Detection {
	lines := agent.TrimScreen(s.Lines)
	if ch, ok := a.card(lines); ok {
		return agent.Detection{State: agent.StateAsking, Activity: agent.Shorten(agent.QuestionAbove(lines, ch.Start), maxActivity)}
	}
	if w, ok := a.workingLine(lines); ok {
		d := agent.Detection{State: agent.StateWorking, Elapsed: agent.FindElapsed(w)}
		text := w
		if r := []rune(text); len(r) > 1 && r[1] == ' ' && a.P.Spinner.MatchString(string(r[0])) {
			text = string(r[2:])
		}
		if i := strings.Index(text, " ("); i >= 0 {
			text = text[:i]
		}
		d.Activity = strings.TrimSpace(text)
		return d
	}
	if a.inputIndex(lines) >= 0 {
		return agent.Detection{State: agent.StateIdle}
	}
	return agent.Detection{State: agent.StateUnknown}
}
