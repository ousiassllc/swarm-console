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

// cardSpan は質問カードの枠の上枠と下枠の行の位置。
type cardSpan struct{ top, bottom int }

// afterCardMax は、質問カードの下枠のあとに続けてよい行の数の上限（活動行・タイトル・ステータス行・入力欄の下枠と空行）。
const afterCardMax = 10

// liveCard は入力待ちの質問カード（画面の最後の枠が Ask の枠）の位置を返す（なければ ok=false）。
// 画面で最後の枠（最後の `╭` の行）の上枠が Ask の目印に当たり、その下枠（`╰──`）が画面の下のほうにあるとき。
// v18 までは下枠が画面の最後の行だったが、v19 は枠のあとに活動行・ステータス行・入力欄の下枠（`╰─`）が続く。
// 結果として残った Ask の枠は上枠が `╭─── Ask`（`─` が 3 つ以上）で目印に当たらず、あとの枠が最後の枠になるので当たらない。
func (a *adapter) liveCard(lines []string) (cardSpan, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(lines[i], "╭") {
			continue
		}
		if !a.P.Ask.MatchString(lines[i]) {
			return cardSpan{}, false
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "╰──") {
				return cardSpan{top: i, bottom: j}, len(lines)-1-j <= afterCardMax
			}
		}
		return cardSpan{}, false
	}
	return cardSpan{}, false
}

// Detect は画面の状態を判定する。質問カード → 作業中 → 入力待ちの順に見る。
func (a *adapter) Detect(s agent.Screen) agent.Detection {
	lines := agent.TrimScreen(s.Lines)
	if span, ok := a.liveCard(lines); ok {
		return agent.Detection{State: agent.StateAsking, Activity: a.askActivity(lines, span)}
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

// askActivity は質問中の活動行（`<記号> Asking …`）の文言。v19 は枠の下（ステータス行の直上）、v18 は枠の上にある。
func (a *adapter) askActivity(lines []string, span cardSpan) string {
	if idx := a.statusIndex(lines); idx > span.bottom {
		if t := activityAbove(lines, idx); t != "" {
			return t
		}
	}
	return activityAbove(lines, span.top)
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
