package nativetest

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// 受け入れ条件：tern の画面と普通の画面で、状態の判定・ask_card・pane_tail が同じ結果になる。

// 状態（idle・working・asking）。tern の画面は会話の記録の行数、普通の画面は実画面。
func TestStateIsTheSameOnTernAndClassicScreens(t *testing.T) {
	cases := []struct {
		name    string
		lines   int    // fixture の何行目まで書いたか
		classic string // 普通の画面（testdata/screens/omp）
		fixture string // 同じ場面の画面が testdata/native/omp にあるとき
		want    agent.State
	}{
		{"idle", 3, "idle.txt", "", agent.StateIdle},
		{"working", 4, "working.txt", "", agent.StateWorking},
		{"長いコマンド（ツールの呼び出しで止まったまま）", 10, "working-todo.txt", "", agent.StateWorking},
		{"asking", 6, "", "classic-ask.txt", agent.StateAsking},
		{"質問に答えたあと", 8, "idle-after-ask-v19.txt", "", agent.StateIdle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, c.lines, nil, nil, nil)
			if c.fixture != "" {
				g.setClassic(classicFixture(t, c.fixture))
			} else {
				g.setClassic(classicSample(t, c.classic))
			}
			n, cl := g.states()
			if n.State != c.want || cl.State != c.want {
				t.Fatalf("state: tern=%s classic=%s, want %s", n.State, cl.State, c.want)
			}
			if n.Agent != "omp" || cl.Agent != "omp" {
				t.Errorf("agent: tern=%q classic=%q", n.Agent, cl.Agent)
			}
			if (n.Activity != "") != (cl.Activity != "") {
				t.Errorf("activity: tern=%q classic=%q（あり・なしが違う）", n.Activity, cl.Activity)
			}
		})
	}
}

// tern の画面のペインの title は、画面データ（最後のフレーム）ではなく空。普通の画面は pane_title のまま。
func TestTernPaneTitleIsNotExposed(t *testing.T) {
	g := newRig(t, 3, nil, nil, nil)
	g.setClassic(classicSample(t, "idle.txt"))
	n, cl := g.states()
	if n.Title != "" || strings.Contains(n.Title, "tsp;") {
		t.Errorf("tern の画面の title = %q", n.Title)
	}
	if cl.Title != "π > work" {
		t.Errorf("普通の画面の title = %q, want そのまま", cl.Title)
	}
}

// ask_card：質問・選択肢・カーソル・自由入力の可否が、同じ質問の普通の画面と同じ。
func TestAskCardIsTheSameOnTernAndClassicScreens(t *testing.T) {
	g := newRig(t, 6, nil, nil, nil)
	g.setClassic(classicFixture(t, "classic-ask.txt"))
	n, cl := g.states()
	if n.State != agent.StateAsking || cl.State != agent.StateAsking {
		t.Fatalf("state: tern=%s classic=%s", n.State, cl.State)
	}
	nc, err := g.reg.Card(context.Background(), refOf(n))
	if err != nil {
		t.Fatal(err)
	}
	cc, err := g.reg.Card(context.Background(), refOf(cl))
	if err != nil {
		t.Fatal(err)
	}
	if nc == nil || cc == nil {
		t.Fatalf("card: tern=%v classic=%v", nc, cc)
	}
	if nc.Question != cc.Question || nc.Cursor != cc.Cursor || nc.AllowsCustom != cc.AllowsCustom || len(nc.Options) != len(cc.Options) {
		t.Fatalf("card\n tern=%+v\n classic=%+v", *nc, *cc)
	}
	for i := range cc.Options {
		if nc.Options[i] != cc.Options[i] {
			t.Errorf("Options[%d]: tern=%+v classic=%+v", i, nc.Options[i], cc.Options[i])
		}
	}
	if n.Activity != cl.Activity {
		t.Errorf("activity（質問の要約）: tern=%q classic=%q", n.Activity, cl.Activity)
	}
}

// 質問でないときの ask_card は、どちらも nil（not asking）。
func TestAskCardIsNilWhenNotAsking(t *testing.T) {
	g := newRig(t, 3, nil, nil, nil)
	g.setClassic(classicSample(t, "idle.txt"))
	n, cl := g.states()
	for _, p := range []pane.PaneState{n, cl} {
		if c, err := g.reg.Card(context.Background(), refOf(p)); err != nil || c != nil {
			t.Errorf("%s: card=%v err=%v", p.ID, c, err)
		}
	}
}

// pane_tail：人が読める中身（指示・応答・ツールの呼び出し）を返し、tern の画面データ（tsp;f;{...}）や飾りを含めない。
func TestPaneTailIsReadableOnTernScreen(t *testing.T) {
	g := newRig(t, 13, nil, nil, nil)
	g.setClassic(classicSample(t, "idle.txt"))
	n, cl := g.states()
	tail, err := g.reg.Tail(context.Background(), refOf(n), 50, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Reply with exactly the single word OK", "Blue.", "▸ bash: Sleeping then writing report"} {
		if !strings.Contains(tail.Text, want) {
			t.Errorf("tern の pane_tail に %q が無い:\n%s", want, tail.Text)
		}
	}
	for _, bad := range []string{"tsp;", `"ops"`, "tern"} {
		if strings.Contains(tail.Text, bad) {
			t.Errorf("tern の pane_tail に %q が出ている:\n%s", bad, tail.Text)
		}
	}
	raw, err := g.reg.Tail(context.Background(), refOf(n), 50, true)
	if err != nil || !strings.Contains(raw.Text, "Blue.") {
		t.Errorf("raw の pane_tail: %v\n%s", err, raw.Text)
	}
	if ctail, err := g.reg.Tail(context.Background(), refOf(cl), 50, false); err != nil || ctail.Text == "" {
		t.Errorf("普通の画面の pane_tail が変わった: %v %q", err, ctail.Text)
	}
}

// 画面ハッシュ・changed_at：tern の画面は経過時間の更新では変わらず、会話が進むと変わる（stalled の判定が効く）。
func TestScreenHashChangesWithConversationOnly(t *testing.T) {
	g := newRig(t, 11, nil, nil, nil)
	g.setClassic(classicSample(t, "idle.txt"))
	n1, _ := g.states()
	g.env.Clock.Advance(3_000_000_000)
	n2, _ := g.states()
	if n1.ScreenHash != n2.ScreenHash {
		t.Error("会話が進んでいないのに画面ハッシュが変わった")
	}
	g.appendFixture(12)
	n3, _ := g.states()
	if n3.ScreenHash == n2.ScreenHash {
		t.Error("会話が進んだのに画面ハッシュが変わらない")
	}
}

func refOf(p pane.PaneState) pane.Ref { return pane.Ref{ID: p.ID, Pos: p.Pos, Local: p.Local} }
