package omp

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

var (
	// 選択肢の行: 行頭の余白、任意のカーソル、記号（○・☐・☑）、文言。説明の続き行は記号がないので当たらない。
	optionRE      = regexp.MustCompile(`^\s?(` + string(cursorGlyph) + `)?\s*\p{Co}\s+(\S.*?)\s*$`)
	tabRE         = regexp.MustCompile(`\sSubmit$`)
	customRE      = regexp.MustCompile(`^Other \(type your own\)$`)
	recommendedRE = regexp.MustCompile(`(?i)\s*\(recommended\)\s*$`)
)

// ParseCard は質問カード（`╭─ Ask`）から質問・選択肢・カーソルを読む。
// 「Other (type your own)」は Options に入れず AllowsCustom にする（カーソルがそこなら Cursor=len(Options)+1）。
// 自由入力中（`Custom answer`）の枠は選ぶ対象がないので ok=false。
func (a *adapter) ParseCard(s agent.Screen) (agent.Card, bool) {
	lines := agent.TrimScreen(s.Lines)
	top := a.liveCard(lines)
	if top < 0 || strings.Contains(lines[top], "Custom answer") {
		return agent.Card{}, false
	}
	var head, opts []string
	section := 0 // 0=見出し 1=選択肢 2=フッター
	for _, l := range lines[top+1 : len(lines)-1] {
		if strings.HasPrefix(l, "├") {
			section++
			continue
		}
		inner, ok := agent.BoxInner(l)
		if !ok {
			continue
		}
		switch section {
		case 0:
			if t := strings.TrimSpace(inner); t != "" && !tabRE.MatchString(t) {
				head = append(head, t)
			}
		case 1:
			opts = append(opts, inner)
		}
	}
	c := agent.Card{Question: strings.Join(head, " ")}
	for _, inner := range opts {
		m := optionRE.FindStringSubmatch(inner)
		if m == nil {
			continue
		}
		label := recommendedRE.ReplaceAllString(m[2], "")
		isCursor := m[1] != ""
		if customRE.MatchString(label) {
			c.AllowsCustom = true
			if isCursor {
				c.Cursor = len(c.Options) + 1
			}
			continue
		}
		c.Options = append(c.Options, agent.Option{N: len(c.Options) + 1, Label: label, Recommended: recommendedRE.MatchString(m[2])})
		if isCursor {
			c.Cursor = len(c.Options)
		}
	}
	return c, len(c.Options) > 0
}

// PickKeys は n 番へカーソルを動かす Up/Down を差分だけ返す（Enter は含めない）。
func (a *adapter) PickKeys(c agent.Card, n int) ([]agent.Key, error) {
	if err := agent.CheckPick(c, n); err != nil {
		return nil, err
	}
	return agent.ArrowKeys(c.Cursor, n)
}

// CustomKeys は「Other (type your own)」へカーソルを動かす Down を返す。
func (a *adapter) CustomKeys(c agent.Card) ([]agent.Key, error) {
	if !c.AllowsCustom {
		return nil, apperr.New(apperr.CustomNotAllowed)
	}
	return agent.ArrowKeys(c.Cursor, len(c.Options)+1)
}
