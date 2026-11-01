package claude

import (
	"regexp"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

var (
	// 自由入力の項（AskUserQuestion の「Type something.」）。Options に入れず AllowsCustom にする。
	customRE = regexp.MustCompile(`(?i)^(?:Type something\.?|Other\b.*)$`)
	// 選べる項ではない「Chat about this」。
	chatRE = regexp.MustCompile(`(?i)^Chat about this$`)
)

// tidyLabel は /model メニューのように文言のあとに説明が続く項から、文言だけを取り出す。
func tidyLabel(l string) string {
	if i := strings.Index(l, "  "); i >= 0 {
		l = l[:i]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l), "✔"))
}

// ParseCard は `❯` つきの選択カード（信頼確認・許可確認・質問・/model メニュー）を読む。
func (a *adapter) ParseCard(s agent.Screen) (agent.Card, bool) {
	lines := agent.TrimScreen(s.Lines)
	ch, ok := a.card(lines)
	if !ok {
		return agent.Card{}, false
	}
	keep := ch.Labels[:0:0]
	for _, l := range ch.Labels {
		if !chatRE.MatchString(tidyLabel(l)) {
			keep = append(keep, l)
		}
	}
	ch.Labels = keep
	c := agent.BuildCard(lines, ch, customRE, tidyLabel)
	return c, len(c.Options) > 0
}

// PickKeys は n 番へカーソルを動かす。カーソルが読めているときは Up/Down の差分（数字キーは押した時点で
// 確定する画面があり、送ったあとに検証できないため）、読めないときだけ数字キー 1 つにする。Enter は含めない。
func (a *adapter) PickKeys(c agent.Card, n int) ([]agent.Key, error) {
	if err := agent.CheckPick(c, n); err != nil {
		return nil, err
	}
	return agent.ArrowOrDigit(c.Cursor, n)
}

// CustomKeys は自由入力の項（Options の次の番号）へ動かすキー列を返す。項がなければ custom_not_allowed。
func (a *adapter) CustomKeys(c agent.Card) ([]agent.Key, error) {
	if !c.AllowsCustom {
		return nil, apperr.New(apperr.CustomNotAllowed)
	}
	return agent.ArrowOrDigit(c.Cursor, len(c.Options)+1)
}
