package panetest

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// FakeAdapter は画面の約束ごとだけで動く agent.Adapter。画面の作り方は Screen* を使う。
//
//	WORKING を含む行 → working、ASK を含む行 → asking、それ以外 → idle
//	"ACT: ..." 行 → Activity、"EL: ..." 行 → Elapsed、"Q: ..." 行 → 質問
//	"[ ] 1 label" / "[>] 2 label" → 選択肢（> がカーソル）、"[ ] other" / "[>] other" → 自由入力の項
//	"SPINNER" を含む行は StableText から除く、"STATUSLINE" を含む行は Clean で除く
type FakeAdapter struct {
	AgentName string
}

var _ agent.Adapter = FakeAdapter{}

var optRE = regexp.MustCompile(`^\[( |>)\] (\d+|other)(?: (.*))?$`)

// Name はアダプタ名。
func (a FakeAdapter) Name() string { return a.AgentName }

// StartCommand は起動コマンド（名前と同じ）。
func (a FakeAdapter) StartCommand() string { return a.AgentName }

// ExitCommand は終了コマンド。
func (a FakeAdapter) ExitCommand() string { return "/exit" }

// MatchProcess は argv[0] のベース名が名前と同じか。
func (a FakeAdapter) MatchProcess(argv []string) bool {
	return len(argv) > 0 && filepath.Base(argv[0]) == a.AgentName
}

// MatchScreen は "[<name>]" の目印があるか。
func (a FakeAdapter) MatchScreen(s agent.Screen) bool {
	return contains(s.Lines, "["+a.AgentName+"]")
}

func contains(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func prefixed(lines []string, p string) string {
	for _, l := range lines {
		if rest, ok := strings.CutPrefix(l, p); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// Detect は約束ごとの目印から状態を決める。
func (a FakeAdapter) Detect(s agent.Screen) agent.Detection {
	d := agent.Detection{State: agent.StateIdle, Activity: prefixed(s.Lines, "ACT:"), Elapsed: prefixed(s.Lines, "EL:")}
	switch {
	case contains(s.Lines, "ASK"):
		d.State = agent.StateAsking
	case contains(s.Lines, "WORKING"):
		d.State = agent.StateWorking
	}
	return d
}

// ParseCard は質問カードの約束ごとを読む。選択肢が無ければ ok=false（自由入力欄が開いている状態など）。
func (a FakeAdapter) ParseCard(s agent.Screen) (agent.Card, bool) {
	if !contains(s.Lines, "ASK") {
		return agent.Card{}, false
	}
	c := agent.Card{Question: prefixed(s.Lines, "Q:")}
	for _, l := range s.Lines {
		m := optRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		cursor := m[1] == ">"
		if m[2] == "other" {
			c.AllowsCustom = true
			if cursor {
				c.Cursor = len(c.Options) + 1
			}
			continue
		}
		n, _ := strconv.Atoi(m[2])
		c.Options = append(c.Options, agent.Option{N: n, Label: m[3]})
		if cursor {
			c.Cursor = n
		}
	}
	return c, len(c.Options) > 0
}

func moveKeys(from, to int) []agent.Key {
	var keys []agent.Key
	for ; from < to; from++ {
		keys = append(keys, agent.Key{Name: "Down"})
	}
	for ; from > to; from-- {
		keys = append(keys, agent.Key{Name: "Up"})
	}
	return keys
}

// PickKeys はカーソルを n へ動かす Up/Down。
func (a FakeAdapter) PickKeys(c agent.Card, n int) ([]agent.Key, error) {
	if n < 1 || n > len(c.Options) {
		return nil, apperr.New(apperr.OptionOutOfRange)
	}
	return moveKeys(c.Cursor, n), nil
}

// CustomKeys は自由入力の項（Options の次）へ動かす。
func (a FakeAdapter) CustomKeys(c agent.Card) ([]agent.Key, error) {
	if !c.AllowsCustom {
		return nil, apperr.New(apperr.CustomNotAllowed)
	}
	return moveKeys(c.Cursor, len(c.Options)+1), nil
}

// Clean は STATUSLINE を含む行を除く。
func (a FakeAdapter) Clean(lines []string) []string {
	var out []string
	for _, l := range lines {
		if !strings.Contains(l, "STATUSLINE") {
			out = append(out, l)
		}
	}
	return out
}

// StableText は SPINNER を含む行を除いた本文。
func (a FakeAdapter) StableText(lines []string) string {
	var out []string
	for _, l := range lines {
		if !strings.Contains(l, "SPINNER") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// 以下は画面の組み立て。

// ScreenIdle は入力待ちの画面。
func ScreenIdle(name string) string { return "[" + name + "] ready\n> " }

// ScreenWorking は作業中の画面。spin を変えても StableText は変わらない。
func ScreenWorking(name, spin, body string) string {
	return "[" + name + "] WORKING SPINNER" + spin + "\nACT: " + body + "\n> "
}

// ScreenShell はシェルのプロンプトだけの画面。
func ScreenShell() string { return "user@host:~$ " }

// ScreenAsk は質問カード。cursor は 1 始まり（Options の数+1 が自由入力の項）。custom=false なら自由入力の項を出さない。
func ScreenAsk(name, question string, cursor int, custom bool, labels ...string) string {
	b := strings.Builder{}
	b.WriteString("[" + name + "] ASK\nQ: " + question + "\n")
	mark := func(on bool) string {
		if on {
			return "[>]"
		}
		return "[ ]"
	}
	for i, l := range labels {
		b.WriteString(mark(cursor == i+1) + " " + strconv.Itoa(i+1) + " " + l + "\n")
	}
	if custom {
		b.WriteString(mark(cursor == len(labels)+1) + " other\n")
	}
	return b.String()
}
