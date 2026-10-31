// Package claude は Claude Code の画面を読むアダプタ。判定の根拠は internal/agent/PATTERNS.md を参照。
package claude

import (
	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Name はアダプタ名。
const Name = "claude"

// defaults は組み込みの既定値。idle・信頼確認・/model メニューは実画面、作業中・許可確認・質問は推定（PATTERNS.md）。
var defaults = agent.Spec{
	Name:  Name,
	Start: "claude",
	Exit:  "/exit",
	Src: agent.PatternSrc{
		ProcessMatch: []string{`(^|/)claude$`},
		// 作業中は「(12s · ↓ 340 tokens · esc to interrupt)」付きのスピナー行が出る。
		Working: `esc to interrupt`,
		// 選択カードの案内文（ここに当たるものがカーソル記号つきの選択肢と一緒にあれば質問）。
		Ask:          `(?i)\b(?:Esc to cancel|Enter to (?:select|confirm|set)|Do you want to|Would you like to)\b`,
		Idle:         `^\s*[❯>](?:[\s\x{a0}]|$)`,
		StatusLine:   `^\s*(?:[⏵⏸]|\? for shortcuts|esc to interrupt\b|Not logged in\b)`,
		Border:       `^\s*(?:[─━▔]{3,}|[╭├╰]─)`,
		SpinnerChars: "✻✢✳✶✽✦·*",
	},
}

type adapter struct{ *agent.Base }

func init() {
	agent.Register(Name, New)
}

// New は ov を適用した Claude Code アダプタを作る。
func New(ov agent.Overrides) (agent.Adapter, error) {
	b, err := agent.NewBase(defaults, ov)
	if err != nil {
		return nil, err
	}
	return &adapter{Base: b}, nil
}

// MatchScreen は入力欄の枠・案内の文言・ロゴのどれかが見えるかで Claude Code の画面と判定する。
func (a *adapter) MatchScreen(s agent.Screen) bool {
	lines := agent.TrimScreen(s.Lines)
	if _, ok := a.card(lines); ok {
		return true
	}
	if a.inputIndex(lines) >= 0 {
		return true
	}
	for _, l := range lines {
		if logoRE.MatchString(l) {
			return true
		}
	}
	return false
}
