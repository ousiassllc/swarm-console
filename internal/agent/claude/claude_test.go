package claude_test

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/claude"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

const (
	trustQ = "Quick safety check: Is this a project you created or one you trust? (Like your own code, a well-known open source project, or work from your team). If not, take a moment to review what's in this folder first."
	modelQ = "Select model Switch between Claude models. Your pick becomes the default for new sessions. For other/previous model names, specify with --model."
)

func trust(cursor int) *agent.Card {
	return &agent.Card{
		Question: trustQ,
		Options:  []agent.Option{{N: 1, Label: "No, exit"}, {N: 2, Label: "Yes, I trust this folder"}},
		Cursor:   cursor,
	}
}

func model(cursor int) *agent.Card {
	return &agent.Card{
		Question: modelQ,
		Options: []agent.Option{
			{N: 1, Label: "Default", Recommended: true}, {N: 2, Label: "Opus"}, {N: 3, Label: "Fable"},
			{N: 4, Label: "Sonnet"}, {N: 5, Label: "Haiku"},
		},
		Cursor: cursor,
	}
}

func permission(cursor int) *agent.Card {
	return &agent.Card{
		Question: "Do you want to proceed?",
		Options: []agent.Option{
			{N: 1, Label: "Yes"},
			{N: 2, Label: "Yes, and don’t ask again for touch commands in /tmp/sc-probe/wd"},
			{N: 3, Label: "No, and tell Claude what to do differently (esc)"},
		},
		Cursor: cursor,
	}
}

func question(cursor int) *agent.Card {
	return &agent.Card{
		Question: "Pick a fruit?",
		Options: []agent.Option{
			{N: 1, Label: "Apple"}, {N: 2, Label: "Banana", Recommended: true}, {N: 3, Label: "Cherry"},
		},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

func TestScreens(t *testing.T) {
	ask := func(f string, c *agent.Card) agenttest.Row {
		return agenttest.Row{File: f, State: agent.StateAsking, Activity: agent.Shorten(c.Question, 80), Card: c}
	}
	agenttest.CheckRows(t, agenttest.New(t, "claude"), "claude", []agenttest.Row{
		{File: "idle.txt", State: agent.StateIdle},
		{File: "idle-bypass.txt", State: agent.StateIdle},
		// 「Cogitated for 0s · done」は終わった印で、作業中ではない。
		{File: "idle-after-error.txt", State: agent.StateIdle},
		{File: "working.txt", State: agent.StateWorking, Activity: "Cogitating…", Elapsed: "12s"},
		{File: "working-b.txt", State: agent.StateWorking, Activity: "Pondering…", Elapsed: "13s"},
		ask("trust-no.txt", trust(1)),
		ask("trust-yes.txt", trust(2)),
		ask("model-menu-2.txt", model(2)),
		ask("model-menu-3.txt", model(3)),
		ask("permission-1.txt", permission(1)),
		ask("permission-2.txt", permission(2)),
		ask("permission-3.txt", permission(3)),
		ask("question-1.txt", question(1)),
		ask("question-2.txt", question(2)),
		ask("question-3.txt", question(3)),
		// カーソルが「Type something.」にあるので Cursor は len(Options)+1。「Chat about this」は選択肢に数えない。
		ask("question-4.txt", question(4)),
	})
}

func TestKeys(t *testing.T) {
	agenttest.CheckKeys(t, agenttest.New(t, "claude"), "claude", []agenttest.KeyRow{
		{File: "trust-no.txt", N: 2, Want: []string{"Down"}},
		{File: "trust-yes.txt", N: 1, Want: []string{"Up"}},
		{File: "trust-yes.txt", N: 2, Want: []string{}},
		{File: "model-menu-2.txt", N: 5, Want: []string{"Down", "Down", "Down"}},
		{File: "permission-3.txt", N: 1, Want: []string{"Up", "Up"}},
		{File: "question-2.txt", N: 0, Want: []string{"Down", "Down"}},
		{File: "question-4.txt", N: 0, Want: []string{}},
		{File: "question-4.txt", N: 1, Want: []string{"Up", "Up", "Up"}},
		{File: "permission-1.txt", N: 0, ErrCode: apperr.CustomNotAllowed},
		{File: "permission-1.txt", N: 4, ErrCode: apperr.OptionOutOfRange},
		{File: "trust-no.txt", N: 0, ErrCode: apperr.CustomNotAllowed},
	})
}

func TestDigitFallbackWhenCursorUnknown(t *testing.T) {
	a := agenttest.New(t, "claude")
	c := *permission(0)
	keys, err := a.PickKeys(c, 2)
	if err != nil || len(keys) != 1 || keys[0].Name != "2" {
		t.Fatalf("keys = %v, err = %v", keys, err)
	}
	c.Options = make([]agent.Option, 10)
	if _, err := a.PickKeys(c, 10); apperr.As(err).Code != apperr.OptionOutOfRange {
		t.Fatalf("10 番は数字 1 キーで押せない: %v", err)
	}
}

func TestStableText(t *testing.T) {
	agenttest.CheckStable(t, agenttest.New(t, "claude"), "claude",
		[][2]string{{"working.txt", "working-b.txt"}},
		[][2]string{{"idle.txt", "working.txt"}, {"permission-1.txt", "permission-2.txt"}, {"trust-no.txt", "trust-yes.txt"}})
}

func TestClean(t *testing.T) {
	a := agenttest.New(t, "claude")
	drop := []string{"────", "▔▔▔", "esc to interrupt", "shortcuts", "bypass permissions", "Try \""}
	agenttest.CheckCleanHas(t, a, "claude", "idle.txt", []string{"Claude Code v2.1.316"}, drop)
	agenttest.CheckCleanHas(t, a, "claude", "working.txt", []string{"Use the Bash tool"}, append(drop, "Cogitating"))
	agenttest.CheckCleanHas(t, a, "claude", "permission-1.txt", []string{"Bash command", "Do you want to proceed?", "1. Yes"}, drop)
}

func TestProcessAndCommands(t *testing.T) {
	a := agenttest.New(t, "claude")
	for argv, want := range map[string]bool{
		"claude":                    true,
		"/home/u/.local/bin/claude": true,
		"claude-helper":             false,
		"vim":                       false,
	} {
		if got := a.MatchProcess([]string{argv, "--permission-mode", "default"}); got != want {
			t.Errorf("MatchProcess(%q) = %v", argv, got)
		}
	}
	if !a.MatchProcess([]string{"node", "/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js/claude"}) {
		t.Error("node 経由の起動を判別できません")
	}
	if a.Name() != "claude" || a.StartCommand() != "claude" || a.ExitCommand() != "/exit" {
		t.Errorf("コマンド名: %s %s %s", a.Name(), a.StartCommand(), a.ExitCommand())
	}
}

func TestMatchScreen(t *testing.T) {
	a := agenttest.New(t, "claude")
	if !a.MatchScreen(agenttest.Load(t, "claude", "idle.txt")) || !a.MatchScreen(agenttest.Load(t, "claude", "trust-no.txt")) {
		t.Error("Claude Code の画面を判別できません")
	}
	if a.MatchScreen(agent.NewScreen([]string{"$ ls", "file.txt", "$"})) {
		t.Error("シェルの画面を Claude Code と判別しました")
	}
}
