package omp

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// pasteMenuRE は貼り付けの確認メニューの上枠（`╭─ Pasted 159 lines ───…`。testdata/screens/omp/paste-prompt.txt）。
var pasteMenuRE = regexp.MustCompile(`^╭─ Pasted \d+ lines\b`)

var _ agent.PasteMenu = (*adapter)(nil)

// PasteMenuOpen は画面の最後の枠が貼り付けの確認メニューか。
// omp は設定 paste.largeMenuThreshold（既定 100）行以上の貼り付けでこのメニューを開く。
// 開いている間の Enter は「Attach as a wrapped block」の選択になり、依頼は送信されない（Issue #99）。
func (a *adapter) PasteMenuOpen(s agent.Screen) bool {
	lines := agent.TrimScreen(s.Lines)
	if len(lines) == 0 || !strings.HasPrefix(lines[len(lines)-1], "╰") {
		return false
	}
	for i := len(lines) - 2; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "╭") {
			return pasteMenuRE.MatchString(lines[i])
		}
	}
	return false
}

// PasteMenuClose は Esc。メニューのヘルプ行は「⎋ to paste inline」で、貼った本文をそのまま入力欄へ載せる（タグで包まない）。
func (a *adapter) PasteMenuClose() agent.Key { return agent.Key{Name: "Escape"} }
