package allagents_test

import (
	"slices"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/allagents"
)

func ptr(s string) *string { return &s }

func TestRegistered(t *testing.T) {
	if got := agent.Names(); !slices.Equal(got, []string{"claude", "codex", "omp"}) {
		t.Fatalf("Names = %v", got)
	}
}

// 不正な正規表現は、どのアダプタ・どのキーでも Factory のエラーになる。
func TestInvalidRegexpIsFactoryError(t *testing.T) {
	const bad = `(unclosed`
	keys := map[string]agent.Overrides{
		"working_re":     {WorkingRE: ptr(bad)},
		"ask_re":         {AskRE: ptr(bad)},
		"idle_re":        {IdleRE: ptr(bad)},
		"status_line_re": {StatusLineRE: ptr(bad)},
		"border_re":      {BorderRE: ptr(bad)},
		"process_match":  {ProcessMatch: []string{`ok`, bad}},
	}
	for _, name := range agent.Names() {
		for key, ov := range keys {
			if a, err := agent.New(name, ov); err == nil {
				t.Errorf("%s/%s: エラーになるはずが成功した (%v)", name, key, a)
			}
		}
	}
	if _, err := agent.New("nope", agent.Overrides{}); err == nil {
		t.Error("未登録のアダプタはエラーになるはず")
	}
}

// 空の上書きは既定値のまま、書かれたキーだけが置き換わる。
func TestOverrideAppliesOnlyGivenKeys(t *testing.T) {
	for _, name := range agent.Names() {
		base := agenttest.New(t, name)
		a, err := agent.New(name, agent.Overrides{ProcessMatch: []string{`^mytool$`}, ExitCommand: ptr("/bye")})
		if err != nil {
			t.Fatal(err)
		}
		if !a.MatchProcess([]string{"mytool"}) || a.MatchProcess([]string{name}) {
			t.Errorf("%s: process_match が置き換わっていない", name)
		}
		if a.ExitCommand() != "/bye" || a.StartCommand() != base.StartCommand() {
			t.Errorf("%s: exit=%s start=%s", name, a.ExitCommand(), a.StartCommand())
		}
		if !base.MatchProcess([]string{name}) {
			t.Errorf("%s: 既定の process_match が効いていない", name)
		}
		same, err := agent.New(name, agent.Overrides{ProcessMatch: []string{}, WorkingRE: ptr("")})
		if err != nil || !same.MatchProcess([]string{name}) {
			t.Errorf("%s: 空の上書きは既定値のまま: %v", name, err)
		}
	}
}

// working_re を上書きすると、画面の判定が設定だけで変わる（FR-AD-2）。
func TestWorkingREOverrideChangesDetect(t *testing.T) {
	cases := map[string]string{"omp": "working.txt", "claude": "working.txt", "codex": "working.txt"}
	for name, file := range cases {
		s := agenttest.Load(t, name, file)
		if got := agenttest.New(t, name).Detect(s).State; got != agent.StateWorking {
			t.Fatalf("%s: 既定で working のはず: %s", name, got)
		}
		a, err := agent.New(name, agent.Overrides{WorkingRE: ptr(`NEVER-MATCHES-ANYTHING`)})
		if err != nil {
			t.Fatal(err)
		}
		if got := a.Detect(s).State; got == agent.StateWorking {
			t.Errorf("%s: working_re を上書きしても working のまま", name)
		}
	}
}

func TestAskREOverrideChangesDetect(t *testing.T) {
	for name, file := range map[string]string{"omp": "ask-2.txt", "claude": "permission-1.txt", "codex": "approval-1.txt"} {
		s := agenttest.Load(t, name, file)
		a, err := agent.New(name, agent.Overrides{AskRE: ptr(`NEVER-MATCHES-ANYTHING`)})
		if err != nil {
			t.Fatal(err)
		}
		if got := a.Detect(s).State; got == agent.StateAsking {
			t.Errorf("%s: ask_re を上書きしても asking のまま", name)
		}
		if _, ok := a.ParseCard(s); ok {
			t.Errorf("%s: ask_re を上書きしてもカードが読める", name)
		}
	}
}

// spinner_chars を上書きすると、スピナーのコマ違いを同一視する文字の集合が変わる。
func TestSpinnerCharsOverride(t *testing.T) {
	a, b := []string{"✻ Done"}, []string{"✢ Done"}
	def := agenttest.New(t, "claude")
	if def.StableText(a) != def.StableText(b) {
		t.Fatal("既定のスピナー文字ではコマ違いが同じ文字列になるはず")
	}
	only, err := agent.New("claude", agent.Overrides{SpinnerChars: ptr("✻")})
	if err != nil {
		t.Fatal(err)
	}
	if only.StableText(a) == only.StableText(b) {
		t.Error("spinner_chars を ✻ だけにしても ✢ が除かれている")
	}
}
