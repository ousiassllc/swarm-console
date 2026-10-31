package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/allagents"
)

// 擬似エージェントの画面の写し（test/fakeagent/screens）は、アダプタのテストが読む testdata/screens と同じ内容でなければならない。
func TestScreensMatchTestdata(t *testing.T) {
	root := agenttest.Root(t)
	n := 0
	err := fs.WalkDir(screens, "screens", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		mine, err := screens.ReadFile(p)
		if err != nil {
			return err
		}
		orig, err := os.ReadFile(filepath.Join(root, "testdata", p)) //nolint:gosec // embed の固定のパスから組んだ testdata のファイル
		if err != nil {
			t.Errorf("%s: testdata/%s を読めません: %v", p, p, err)
			return nil
		}
		if string(mine) != string(orig) {
			t.Errorf("%s が testdata/%s と違います（testdata を直したら写しも直す）", p, p)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("写しを走査できません: n=%d err=%v", n, err)
	}
}

// 状態遷移で出しうるサンプルは全部 embed に入っていて、embed に使われない写しはない。
func TestEveryEmbeddedFrameIsUsed(t *testing.T) {
	used := map[string]bool{}
	for _, a := range agents {
		for _, sc := range scenarios {
			if a == "codex" && sc == "ask-custom" {
				continue
			}
			m := newMachine(a, sc, func(event) {})
			for _, md := range []mode{modeIdle, modeWorking, modeAsk, modeCustom} {
				m.mode = md
				for cur := 1; cur <= 4; cur++ {
					m.cursor = cur
					for tick := 0; tick < 2; tick++ {
						m.tick = tick
						if _, err := sample(a, m.frameFile()); err != nil {
							if md == modeCustom && m.rows.customRow == 0 {
								continue // 自由入力の項がないカードは modeCustom にならない
							}
							if md == modeAsk && cur > m.lastRow() {
								continue
							}
							t.Errorf("%s/%s mode=%d cursor=%d: %v", a, sc, md, cur, err)
						} else {
							used[a+"/"+m.frameFile()] = true
						}
					}
				}
			}
		}
	}
	err := fs.WalkDir(screens, "screens", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if key := strings.TrimPrefix(p, "screens/"); !used[key] {
			t.Errorf("%s はどの状態でも使われません", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func screenOf(t *testing.T, m *machine) agent.Screen {
	t.Helper()
	lines, err := m.Frame()
	if err != nil {
		t.Fatal(err)
	}
	return agent.NewScreen(lines)
}

// 擬似エージェントの画面は、各アダプタの Detect・ParseCard で期待どおりに判定される。
func TestFramesAreRecognisedByAdapters(t *testing.T) {
	for _, name := range agents {
		a := agenttest.New(t, name)
		t.Run(name+"/idle", func(t *testing.T) {
			s := screenOf(t, newMachine(name, "idle", func(event) {}))
			if d := a.Detect(s); d.State != agent.StateIdle {
				t.Errorf("Detect = %+v", d)
			}
			if !a.MatchScreen(s) {
				t.Error("MatchScreen が false")
			}
		})
		t.Run(name+"/working", func(t *testing.T) {
			m := newMachine(name, "working", func(event) {})
			first := screenOf(t, m)
			m.Tick()
			second := screenOf(t, m)
			d1, d2 := a.Detect(first), a.Detect(second)
			if d1.State != agent.StateWorking || d2.State != agent.StateWorking || d1.Elapsed == "" || d1.Elapsed == d2.Elapsed {
				t.Errorf("Detect = %+v / %+v", d1, d2)
			}
			if a.StableText(first.Lines) != a.StableText(second.Lines) {
				t.Error("コマ違いの画面の StableText が違う")
			}
		})
		for _, sc := range []string{"ask", "ask-custom"} {
			if name == "codex" && sc == "ask-custom" {
				continue
			}
			t.Run(name+"/"+sc, func(t *testing.T) {
				m := newMachine(name, sc, func(event) {})
				for cur := 1; cur <= m.lastRow(); cur++ {
					m.cursor = cur
					s := screenOf(t, m)
					if d := a.Detect(s); d.State != agent.StateAsking {
						t.Fatalf("cursor=%d Detect = %+v", cur, d)
					}
					c, ok := a.ParseCard(s)
					if !ok || c.Cursor != cur || len(c.Options) != m.rows.options || c.AllowsCustom != (m.rows.customRow > 0) {
						t.Errorf("cursor=%d ParseCard = %+v ok=%v", cur, c, ok)
					}
				}
			})
		}
	}
}

// 自由入力中の omp の枠は asking のままカードは読めない。入力した本文が画面に出る。
func TestOmpCustomInputFrame(t *testing.T) {
	a := agenttest.New(t, "omp")
	m := newMachine("omp", "ask", func(event) {})
	m.mode, m.typed = modeCustom, []byte("Mango")
	s := screenOf(t, m)
	if d := a.Detect(s); d.State != agent.StateAsking {
		t.Errorf("Detect = %+v", d)
	}
	if _, ok := a.ParseCard(s); ok {
		t.Error("自由入力中にカードは読めないはず")
	}
	if !strings.Contains(strings.Join(s.Lines, "\n"), "│ > Mango") {
		t.Error("入力した本文が画面に出ていない")
	}
}
