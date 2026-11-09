package codex

import (
	"regexp"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// hintRE は選択肢の文言の末尾にある操作の目印 `(y)`・`(a)`・`(esc)`。数字・Enter で選ぶので文言から外す。
var hintRE = regexp.MustCompile(`\s*\((?:[a-z]|esc)\)$`)

// ParseCard は番号つきの承認カードを読む。Codex の承認カードに自由入力の項はない（AllowsCustom は常に false）。
func (a *adapter) ParseCard(s agent.Screen) (agent.Card, bool) {
	lines := agent.TrimScreen(s.Lines)
	ch, ok := a.card(lines)
	if !ok {
		return agent.Card{}, false
	}
	c := agent.BuildCard(lines, ch, nil, func(l string) string { return hintRE.ReplaceAllString(l, "") })
	c.Question = question(lines, ch)
	return c, len(c.Options) > 0
}

// PickKeys は n 番へカーソルを動かす。カーソルが読めているときは Up/Down の差分、読めないときだけ数字キー 1 つにする。
// Enter は含めない。実画面（codex-cli 0.167.0）では数字キーは押した時点で選択が確定する（`3` で「No」が実行された）ので、
// 送ったあとに到達を確かめられず、続けて送る Enter が次の承認カードを承認しかねない。Up/Down はカーソルが動くだけで、Enter で確定する。
func (a *adapter) PickKeys(c agent.Card, n int) ([]agent.Key, error) {
	if err := agent.CheckPick(c, n); err != nil {
		return nil, err
	}
	return agent.ArrowOrDigit(c.Cursor, n)
}

// CustomKeys は自由入力の項がないので、常に custom_not_allowed を返す。
func (a *adapter) CustomKeys(c agent.Card) ([]agent.Key, error) {
	if !c.AllowsCustom {
		return nil, apperr.New(apperr.CustomNotAllowed)
	}
	return agent.ArrowOrDigit(c.Cursor, len(c.Options)+1)
}
