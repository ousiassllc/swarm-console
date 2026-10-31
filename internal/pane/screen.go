package pane

import (
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// SplitLines は capture-pane の出力を行に分ける。末尾の空行は落とす。
func SplitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// ScreenOf は capture-pane の出力をアダプタに渡す Screen にする（最後の 80 行まで）。
func ScreenOf(text string) agent.Screen { return agent.NewScreen(SplitLines(text)) }

// TruncateRunes は s を limit 文字（rune）までに切る。切ったら truncated=true。
func TruncateRunes(s string, limit int) (out string, truncated bool) {
	if limit < 0 {
		limit = 0
	}
	n := 0
	for i := range s {
		if n == limit {
			return s[:i], true
		}
		n++
	}
	return s, false
}

// oneLine は空白・改行の連続を空白 1 つにした 1 行にする。
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
