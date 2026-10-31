// Package codex は Codex CLI の画面を読むアダプタ。この機に codex が無く実画面で確かめていないため、
// 既定値は公開されている TUI の知識による推定（P-22 は未確定。internal/agent/PATTERNS.md）。
package codex

import (
	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Name はアダプタ名。
const Name = "codex"

// defaults は組み込みの既定値（すべて推定）。
var defaults = agent.Spec{
	Name:  Name,
	Start: "codex",
	Exit:  "/quit",
	Src: agent.PatternSrc{
		ProcessMatch: []string{`(^|/)codex$`},
		// 作業中は「• Working (12s • esc to interrupt)」の行が出る。
		Working: `esc to interrupt`,
		// 承認カードの案内文。番号つきの選択肢と一緒にあるときだけ質問とみなす。
		Ask:          `(?i)(?:Press enter to confirm or esc to cancel|Would you like to|Do you want to)`,
		Idle:         `^\s*›(?:[\s\x{a0}]|$)`,
		StatusLine:   `^\s*(?:\d+% context left|\? for shortcuts|Ctrl\+C again to quit)`,
		Border:       `^\s*[╭╰]─`,
		SpinnerChars: "•◦",
	},
}

type adapter struct{ *agent.Base }

func init() {
	agent.Register(Name, New)
}

// New は ov を適用した Codex アダプタを作る。
func New(ov agent.Overrides) (agent.Adapter, error) {
	b, err := agent.NewBase(defaults, ov)
	if err != nil {
		return nil, err
	}
	return &adapter{Base: b}, nil
}

// MatchScreen は入力欄（`› `）と下の案内、起動枠の見出し、承認カードのどれかが見えるかで Codex の画面と判定する。
func (a *adapter) MatchScreen(s agent.Screen) bool {
	lines := agent.TrimScreen(s.Lines)
	if _, ok := a.card(lines); ok {
		return true
	}
	for _, l := range lines {
		if a.P.StatusLine.MatchString(l) || bannerRE.MatchString(l) {
			return true
		}
	}
	return false
}
