package safety

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// CheckText は端末へ貼り付けるテキストを検証する（P-13・NFR-SEC-9）。
// 許可する制御文字は "\n"・"\t" だけ。ESC・NUL・CR・DEL・C1 などと不正な UTF-8 は forbidden_control_char。
// 8000 字（rune）超は text_too_long。空文字は通す（必須かどうかは呼び出し側が決める）。
func (p *Policy) CheckText(s string) error { return checkText(s, config.MaxTextRunes, true) }

// CheckLine は 1 行のテキスト（note_log・タイトルなど）を検証する。CheckText の規則に加えて改行も拒否する。
// maxRunes 字（rune）超は text_too_long（details.max に上限）。
func (p *Policy) CheckLine(s string, maxRunes int) error { return checkText(s, maxRunes, false) }

func checkText(s string, maxRunes int, allowNewline bool) error {
	if !utf8.ValidString(s) {
		return apperr.New(apperr.ForbiddenControlChar, "reason", "invalid_utf8")
	}
	n := 0
	for _, r := range s {
		n++
		if !unicode.IsControl(r) || r == '\t' || (r == '\n' && allowNewline) {
			continue
		}
		return apperr.New(apperr.ForbiddenControlChar, "codepoint", fmt.Sprintf("U+%04X", r))
	}
	if n > maxRunes {
		return apperr.New(apperr.TextTooLong, "max", maxRunes)
	}
	return nil
}
