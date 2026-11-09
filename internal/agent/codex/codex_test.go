package codex_test

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/codex"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

const cmdQ = "Would you like to run the following command?"

// 実画面（codex-cli 0.167.0、120×40。Issue #4）。2 番の文言は長くて折り返し、`(p)` だけの行が続く。
func approval(cursor int) *agent.Card {
	return &agent.Card{
		Question: cmdQ,
		Options: []agent.Option{
			{N: 1, Label: "Yes, proceed"},
			{N: 2, Label: "Yes, and don't ask again for commands that start with `touch /tmp/swarm-console/T2-evidence/approval-probe2.txt`"},
			{N: 3, Label: "No, and tell Codex what to do differently"},
		},
		Cursor: cursor,
	}
}

func TestScreens(t *testing.T) {
	patch := &agent.Card{
		Question: "Would you like to make the following edits?",
		Options:  []agent.Option{{N: 1, Label: "Yes, proceed"}, {N: 2, Label: "No, and tell Codex what to do differently"}},
		Cursor:   1,
	}
	ask := func(f string, c *agent.Card) agenttest.Row {
		return agenttest.Row{File: f, State: agent.StateAsking, Activity: c.Question, Card: c}
	}
	agenttest.CheckRows(t, agenttest.New(t, "codex"), "codex", []agenttest.Row{
		// 入力欄 `› Ask Codex to do anything` は選択肢ではない。
		{File: "idle.txt", State: agent.StateIdle},
		{File: "working.txt", State: agent.StateWorking, Activity: "Working", Elapsed: "0s"},
		{File: "working-b.txt", State: agent.StateWorking, Activity: "Working", Elapsed: "1s"},
		// 本文（`• 指定されたコマンドを実行します。`）が出ている間も、作業中の行が下にあれば作業中。
		{File: "working-activity.txt", State: agent.StateWorking, Activity: "Working", Elapsed: "2s"},
		{File: "idle-after-run.txt", State: agent.StateIdle},
		// 数字キー `3` を押しただけで確定した（キャンセルされた）あとの画面。数字キーはその場で確定する。
		{File: "declined-by-digit.txt", State: agent.StateIdle},
		ask("approval-1.txt", approval(1)),
		ask("approval-2.txt", approval(2)),
		ask("approval-3.txt", approval(3)),
		ask("patch-1.txt", patch),
	})
}

func TestKeys(t *testing.T) {
	agenttest.CheckKeys(t, agenttest.New(t, "codex"), "codex", []agenttest.KeyRow{
		// カーソルが読めるので Up/Down。数字キーは押した時点で確定してしまう（実画面）。
		{File: "approval-1.txt", N: 1, Want: []string{}},
		{File: "approval-1.txt", N: 3, Want: []string{"Down", "Down"}},
		{File: "approval-3.txt", N: 2, Want: []string{"Up"}},
		{File: "approval-2.txt", N: 3, Want: []string{"Down"}},
		{File: "approval-1.txt", N: 4, ErrCode: apperr.OptionOutOfRange},
		{File: "patch-1.txt", N: 3, ErrCode: apperr.OptionOutOfRange},
		{File: "approval-1.txt", N: -1, ErrCode: apperr.OptionOutOfRange},
		{File: "approval-2.txt", N: 0, ErrCode: apperr.CustomNotAllowed},
	})
}

// カーソルが読めないときだけ数字キー 1 つ。
func TestDigitFallbackWhenCursorUnknown(t *testing.T) {
	keys, err := agenttest.New(t, "codex").PickKeys(*approval(0), 2)
	if err != nil || len(keys) != 1 || keys[0].Name != "2" {
		t.Fatalf("keys = %v, err = %v", keys, err)
	}
}

func TestStableText(t *testing.T) {
	agenttest.CheckStable(t, agenttest.New(t, "codex"), "codex",
		[][2]string{{"working.txt", "working-b.txt"}},
		[][2]string{{"idle.txt", "working.txt"}, {"approval-1.txt", "patch-1.txt"}, {"approval-1.txt", "approval-2.txt"}})
}

func TestClean(t *testing.T) {
	a := agenttest.New(t, "codex")
	drop := []string{"╭", "╰", "OpenAI Codex", "context left", "Ask Codex to do anything", "esc to interrupt", "for shortcuts"}
	agenttest.CheckCleanHas(t, a, "codex", "working.txt", []string{"› Run this shell command exactly"}, drop)
	agenttest.CheckCleanHas(t, a, "codex", "approval-2.txt", []string{cmdQ, "› 2. Yes, and don't ask again", "$ touch /tmp/swarm-console/T2-evidence/approval-probe2.txt"}, drop)
}

func TestProcessAndCommands(t *testing.T) {
	a := agenttest.New(t, "codex")
	for argv, want := range map[string]bool{
		"codex":                true,
		"/usr/local/bin/codex": true,
		"codex-helper":         false,
		"vim":                  false,
	} {
		if got := a.MatchProcess([]string{argv, "codex"}); got != want {
			t.Errorf("MatchProcess(%q) = %v", argv, got)
		}
	}
	if !a.MatchProcess([]string{"node", "/usr/lib/node_modules/@openai/codex/bin/codex"}) {
		t.Error("node 経由の起動を判別できません")
	}
	if a.Name() != "codex" || a.StartCommand() != "codex" || a.ExitCommand() != "/quit" {
		t.Errorf("コマンド名: %s %s %s", a.Name(), a.StartCommand(), a.ExitCommand())
	}
}

func TestMatchScreen(t *testing.T) {
	a := agenttest.New(t, "codex")
	for _, f := range []string{"idle.txt", "working.txt", "approval-1.txt"} {
		if !a.MatchScreen(agenttest.Load(t, "codex", f)) {
			t.Errorf("%s を Codex の画面と判別できません", f)
		}
	}
	if a.MatchScreen(agent.NewScreen([]string{"$ ls", "file.txt", "$"})) {
		t.Error("シェルの画面を Codex と判別しました")
	}
}
