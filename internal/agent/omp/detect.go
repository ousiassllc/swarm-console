package omp

import (
	"strings"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

const (
	tailLines     = 8 // ステータス行を探す、画面の下からの行数
	activityIndex = 3 // 活動行の記号までの字下げの上限（これより右はタイトル）
)

// statusIndex は画面の下のほうにあるステータス行の位置（なければ -1）。
func (a *adapter) statusIndex(lines []string) int {
	_, base := agent.Tail(lines, tailLines)
	for i := len(lines) - 1; i >= base; i-- {
		if a.P.StatusLine.MatchString(lines[i]) {
			return i
		}
	}
	return -1
}

// liveCard は入力待ちの質問カード（画面の最後の枠が Ask の枠）の上枠の位置（なければ -1）。
// 結果として残った Ask の枠は、あとにステータス行が続くので当たらない。
func (a *adapter) liveCard(lines []string) int {
	if len(lines) == 0 || !strings.HasPrefix(lines[len(lines)-1], "╰──") {
		return -1
	}
	for i := len(lines) - 2; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "╭") {
			if a.P.Ask.MatchString(lines[i]) {
				return i
			}
			return -1
		}
	}
	return -1
}

// Detect は画面の状態を判定する。質問カード → 作業中 → 入力待ちの順に見る。
func (a *adapter) Detect(s agent.Screen) agent.Detection {
	lines := agent.TrimScreen(s.Lines)
	if top := a.liveCard(lines); top >= 0 {
		return agent.Detection{State: agent.StateAsking, Activity: activityAbove(lines, top)}
	}
	idx := a.statusIndex(lines)
	if idx < 0 {
		return agent.Detection{State: agent.StateUnknown}
	}
	d := agent.Detection{State: agent.StateIdle, Activity: activityAbove(lines, idx)}
	if a.P.Working.MatchString(lines[idx]) {
		d.State = agent.StateWorking
		d.Elapsed = agent.FindElapsed(strings.SplitN(lines[idx], string(statusSep), 2)[0])
	}
	return d
}

// activityAbove は idx の行の直上にある活動行（`<記号> Working…` など）の文言を返す。
// 右寄せのタイトル行（字下げが大きい）や活動行のない画面では ""。
func activityAbove(lines []string, idx int) string {
	if idx < 1 {
		return ""
	}
	l := lines[idx-1]
	if agent.Indent(l) > activityIndex {
		return ""
	}
	t := strings.TrimLeftFunc(l, unicode.IsSpace)
	r := []rune(t)
	if len(r) == 0 || !unicode.Is(unicode.Co, r[0]) {
		return ""
	}
	text := strings.TrimSpace(string(r[1:]))
	if i := strings.Index(text, "  "); i >= 0 {
		text = text[:i]
	}
	return text
}
