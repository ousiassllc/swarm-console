// Package omp は omp（Oh My Pi）の画面を読むアダプタ。判定の根拠は docs/architecture/agent-patterns.md を参照。
package omp

import (
	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Name はアダプタ名。
const Name = "omp"

// 画面の目印のうち、正規表現の上書きの対象にしない固定の記号（docs/architecture/agent-patterns.md）。
const (
	cursorGlyph = '\uf054' // 選択肢の行頭のカーソル（Nerd Font の chevron-right）
	statusCap   = '\ue0b6' // ステータス行の左端（powerline の丸い端）
	statusSep   = '\ue0b1' // ステータス行の区切り
)

// defaults は実画面（testdata/screens/omp）から確定した組み込みの既定値。
var defaults = agent.Spec{
	Name:  Name,
	Start: "omp",
	Exit:  "/exit",
	Src: agent.PatternSrc{
		ProcessMatch: []string{`(^|/)omp$`},
		// ステータス行の左端の直後にスピナー（点字）が出ている間が作業中。
		Working: `^\x{e0b6}\s*[\x{2800}-\x{28ff}]`,
		// 質問カードの上枠。完了した Ask の結果枠（`╭─── Ask`）や進行中の予告枠には当たらない。
		Ask:          `^╭─ (?:Ask|Custom answer)\b`,
		Idle:         `^\x{e0b6}`,
		StatusLine:   `^\x{e0b6}`,
		Border:       `^\s*[╭├╰]─`,
		SpinnerChars: "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏",
	},
}

type adapter struct{ *agent.Base }

func init() {
	agent.Register(Name, New)
}

// New は ov を適用した omp アダプタを作る。
func New(ov agent.Overrides) (agent.Adapter, error) {
	b, err := agent.NewBase(defaults, ov)
	if err != nil {
		return nil, err
	}
	return &adapter{Base: b}, nil
}

// MatchScreen はステータス行か質問カードが見えるかで omp の画面と判定する。
func (a *adapter) MatchScreen(s agent.Screen) bool {
	lines := agent.TrimScreen(s.Lines)
	if a.liveCard(lines) >= 0 {
		return true
	}
	return a.statusIndex(lines) >= 0
}
