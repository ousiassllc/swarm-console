package omp_test

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/omp"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func fruit(cursor int) *agent.Card {
	return &agent.Card{
		Question: "Pick a fruit",
		Options: []agent.Option{
			{N: 1, Label: "Apple"}, {N: 2, Label: "Banana", Recommended: true}, {N: 3, Label: "Cherry"},
		},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

// pickAB は「どちらが好き？」の A・B と Other の 2 択のカード（Issue #20 の実機のカード）。
func pickAB(cursor int) *agent.Card {
	return &agent.Card{
		Question:     "どちらが好き？",
		Options:      []agent.Option{{N: 1, Label: "A"}, {N: 2, Label: "B"}},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

func toppings(cursor int) *agent.Card {
	return &agent.Card{
		Question:     "Pick toppings",
		Options:      []agent.Option{{N: 1, Label: "Cheese"}, {N: 2, Label: "Ham"}, {N: 3, Label: "Olives"}},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

func TestScreens(t *testing.T) {
	const ask = "Asking fruit choice"
	agenttest.CheckRows(t, agenttest.New(t, "omp"), "omp", []agenttest.Row{
		{File: "idle.txt", State: agent.StateIdle},
		{File: "cancelled-idle.txt", State: agent.StateIdle},
		{File: "working.txt", State: agent.StateWorking, Activity: "Working…", Elapsed: "2s"},
		{File: "working-b.txt", State: agent.StateWorking, Activity: "Working…", Elapsed: "3s"},
		{File: "working-todo.txt", State: agent.StateWorking, Activity: "Writing the parser", Elapsed: "2s"},
		{File: "working-after-ask.txt", State: agent.StateWorking, Activity: ask, Elapsed: "2m"},
		{File: "ask-1.txt", State: agent.StateAsking, Activity: ask, Card: fruit(1)},
		{File: "ask-2.txt", State: agent.StateAsking, Activity: ask, Card: fruit(2)},
		{File: "ask-3.txt", State: agent.StateAsking, Activity: ask, Card: fruit(3)},
		{File: "ask-4.txt", State: agent.StateAsking, Activity: ask, Card: fruit(4)},
		{File: "ask-ab-1.txt", State: agent.StateAsking, Activity: ask, Card: pickAB(1)},
		{File: "ask-ab-2.txt", State: agent.StateAsking, Activity: ask, Card: pickAB(2)},
		{File: "ask-ab-3.txt", State: agent.StateAsking, Activity: ask, Card: pickAB(3)},
		{File: "ask-multi.txt", State: agent.StateAsking, Activity: "Asking toppings choice", Card: toppings(1)},
		{File: "ask-multi-toggled.txt", State: agent.StateAsking, Activity: "Asking toppings choice", Card: toppings(2)},
		// 自由入力中の枠は asking のままだが、選ぶ対象がないのでカードは読めない。
		{File: "custom-input.txt", State: agent.StateAsking, Activity: ask},
		{File: "custom-typed.txt", State: agent.StateAsking, Activity: ask},
	})
}

func TestKeys(t *testing.T) {
	agenttest.CheckKeys(t, agenttest.New(t, "omp"), "omp", []agenttest.KeyRow{
		{File: "ask-1.txt", N: 1, Want: []string{}},
		{File: "ask-1.txt", N: 3, Want: []string{"Down", "Down"}},
		{File: "ask-2.txt", N: 1, Want: []string{"Up"}},
		{File: "ask-2.txt", N: 3, Want: []string{"Down"}},
		{File: "ask-3.txt", N: 1, Want: []string{"Up", "Up"}},
		{File: "ask-4.txt", N: 2, Want: []string{"Up", "Up"}},
		{File: "ask-2.txt", N: 0, Want: []string{"Down", "Down"}},
		{File: "ask-4.txt", N: 0, Want: []string{}},
		{File: "ask-ab-1.txt", N: 2, Want: []string{"Down"}},
		{File: "ask-ab-2.txt", N: 1, Want: []string{"Up"}},
		{File: "ask-ab-3.txt", N: 2, Want: []string{"Up"}},
		{File: "ask-ab-1.txt", N: 0, Want: []string{"Down", "Down"}},
		{File: "ask-multi.txt", N: 3, Want: []string{"Down", "Down"}},
		{File: "ask-2.txt", N: 4, ErrCode: apperr.OptionOutOfRange},
		{File: "ask-2.txt", N: -1, ErrCode: apperr.OptionOutOfRange},
	})
}

func TestCustomNotAllowed(t *testing.T) {
	a := agenttest.New(t, "omp")
	c := *fruit(1)
	c.AllowsCustom = false
	if _, err := a.CustomKeys(c); apperr.As(err).Code != apperr.CustomNotAllowed {
		t.Fatalf("err = %v", err)
	}
}

func TestStableText(t *testing.T) {
	agenttest.CheckStable(t, agenttest.New(t, "omp"), "omp",
		[][2]string{{"working.txt", "working-b.txt"}},
		[][2]string{{"working.txt", "working-todo.txt"}, {"ask-1.txt", "ask-2.txt"}, {"idle.txt", "working-after-ask.txt"}})
}

func TestClean(t *testing.T) {
	a := agenttest.New(t, "omp")
	agenttest.CheckClean(t, a, "omp", "working-todo.txt",
		[]string{" Please build the parser and its tests.", "", "\uf129 waiting on 1 job"})
	drop := []string{"\ue0b6", "╭", "╰", "├", "Todo 4 tasks", "Write the parser", "TODO", "devbox"}
	agenttest.CheckCleanHas(t, a, "omp", "working.txt", []string{"Pick a fruit", "You chose a custom answer: Mango."}, drop)
	agenttest.CheckCleanHas(t, a, "omp", "ask-2.txt", []string{"Pick a fruit", "Banana (Recommended)"}, drop)
}

func TestProcessAndCommands(t *testing.T) {
	a := agenttest.New(t, "omp")
	for argv, want := range map[string]bool{
		"omp":                    true,
		"/home/u/.local/bin/omp": true,
		"omp-helper":             false,
		"vim":                    false,
	} {
		if got := a.MatchProcess([]string{argv, "omp"}); got != want {
			t.Errorf("MatchProcess(%q) = %v", argv, got)
		}
	}
	if !a.MatchProcess([]string{"bun", "/opt/omp"}) || a.MatchProcess(nil) {
		t.Error("bun 経由の起動・空の argv の扱いが想定と違います")
	}
	if a.Name() != "omp" || a.StartCommand() != "omp" || a.ExitCommand() != "/exit" {
		t.Errorf("コマンド名: %s %s %s", a.Name(), a.StartCommand(), a.ExitCommand())
	}
}

func TestMatchScreen(t *testing.T) {
	a := agenttest.New(t, "omp")
	if !a.MatchScreen(agenttest.Load(t, "omp", "idle.txt")) {
		t.Error("omp の画面を判別できません")
	}
	if a.MatchScreen(agent.NewScreen([]string{"$ ls", "file.txt", "$"})) {
		t.Error("シェルの画面を omp と判別しました")
	}
}
