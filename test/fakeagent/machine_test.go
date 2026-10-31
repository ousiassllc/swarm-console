package main

import (
	"slices"
	"testing"
)

const (
	up    = "\x1b[A"
	down  = "\x1b[B"
	enter = "\r"
)

func paste(s string) string { return "\x1b[200~" + s + "\x1b[201~" }

func run3(t *testing.T, agent, scenario string, keys ...string) (*machine, []event) {
	t.Helper()
	m, got := newRecorded(agent, scenario)
	for _, k := range keys {
		m.Feed([]byte(k))
	}
	return m, *got
}

func newRecorded(agent, scenario string) (*machine, *[]event) {
	var got []event
	return newMachine(agent, scenario, func(e event) { got = append(got, e) }), &got
}

func TestAskArrowsAndEnter(t *testing.T) {
	tests := []struct {
		name, agent, scenario string
		keys                  []string
		cursor                int
		want                  []event
		mode                  mode
	}{
		{"omp 初期位置は推奨の 2", "omp", "ask", nil, 2, nil, modeAsk},
		{"omp ↓ で 3", "omp", "ask", []string{down}, 3, nil, modeAsk},
		{"omp は下端で止まる（その他が 4）", "omp", "ask", []string{down, down, down, down}, 4, nil, modeAsk},
		{"omp は上端で止まる", "omp", "ask", []string{up, up, up}, 1, nil, modeAsk},
		{"omp Enter で 2 を記録して working", "omp", "ask", []string{enter}, 2, []event{{kind: "PICK", n: 2}}, modeWorking},
		{"omp は数字キーを無視", "omp", "ask", []string{"3"}, 2, nil, modeAsk},
		{"claude 許可確認は 3 で止まる", "claude", "ask", []string{down, down, down}, 3, nil, modeAsk},
		{"claude 数字キーで直接選ぶ", "claude", "ask", []string{"3"}, 3, []event{{kind: "PICK", n: 3}}, modeWorking},
		{"codex 数字 9 は範囲外で無視", "codex", "ask", []string{"9"}, 1, nil, modeAsk},
		{"codex 数字 2", "codex", "ask", []string{"2"}, 2, []event{{kind: "PICK", n: 2}}, modeWorking},
		{"codex は application mode の矢印も受ける", "codex", "ask", []string{"\x1bOB", enter}, 2, []event{{kind: "PICK", n: 2}}, modeWorking},
		{"ESC で質問を取り下げて idle", "omp", "ask", []string{"\x1b"}, 2, []event{{kind: "ESCAPE"}}, modeIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, got := run3(t, tt.agent, tt.scenario, tt.keys...)
			if m.cursor != tt.cursor || m.mode != tt.mode || !slices.Equal(got, tt.want) {
				t.Errorf("cursor=%d mode=%d events=%v, want cursor=%d mode=%d events=%v", m.cursor, m.mode, got, tt.cursor, tt.mode, tt.want)
			}
		})
	}
}

func TestCustomInput(t *testing.T) {
	for _, agent := range []string{"omp", "claude"} {
		sc := "ask"
		if agent == "claude" {
			sc = "ask-custom"
		}
		m, got := newRecorded(agent, sc)
		for _, k := range []string{down, down, enter} { // 初期 2 → 4（その他）
			m.Feed([]byte(k))
		}
		if m.mode != modeCustom || len(*got) != 0 {
			t.Fatalf("%s: その他で入力中になるはず: mode=%d events=%v", agent, m.mode, *got)
		}
		m.Feed([]byte("Mangoo\x7f"))
		m.Feed([]byte(paste("x\ry")))
		m.Feed([]byte(enter))
		want := []event{{kind: "PASTE", text: "x\ny"}, {kind: "CUSTOM", text: "Mangox\ny"}}
		if m.mode != modeWorking || !slices.Equal(*got, want) {
			t.Errorf("%s: mode=%d events=%v, want %v", agent, m.mode, *got, want)
		}
	}
	if _, got := run3(t, "claude", "ask", down, down, down, enter); len(got) != 1 || got[0].n != 3 {
		t.Errorf("自由入力の項がない許可確認では 3 番を選ぶだけ: %v", got)
	}
}

func TestIdlePasteAndExit(t *testing.T) {
	m, got := run3(t, "omp", "idle", paste("line1\nline2"), enter)
	want := []event{{kind: "PASTE", text: "line1\nline2"}, {kind: "SUBMIT"}}
	if m.mode != modeWorking || !slices.Equal(got, want) {
		t.Errorf("貼り付け → Enter は受理して working: mode=%d events=%v", m.mode, got)
	}
	if m, got := run3(t, "omp", "idle", enter, "hello", enter); m.mode != modeIdle || len(got) != 0 {
		t.Errorf("貼り付けなしの Enter は何もしない: mode=%d events=%v", m.mode, got)
	}
	// 貼り付けが 2 回の read にまたがっても 1 件にまとまる。
	_, got = run3(t, "codex", "idle", "\x1b[200~ab", "cd\x1b[201~")
	if len(got) != 1 || got[0].text != "abcd" {
		t.Errorf("分割された貼り付け: %v", got)
	}
	// 貼り付けの中の /exit や ESC は入力として扱わない。
	if m, _ := run3(t, "omp", "idle", paste("/exit\x1b[A"), enter); m.quit {
		t.Error("貼り付けの本文でプロセスが終わった")
	}
	for _, c := range []string{"/exit", "/quit"} {
		if m, _ := run3(t, "claude", "idle", c, enter); !m.quit {
			t.Errorf("%s で終わるはず", c)
		}
	}
	if m, _ := run3(t, "claude", "idle", "/exi", enter); m.quit {
		t.Error("途中の文字列で終わった")
	}
	if m, _ := run3(t, "claude", "working", "/exit", enter); m.quit {
		t.Error("working 中に /exit で終わった")
	}
}

func TestWorkingTickAndEscape(t *testing.T) {
	m, got := newRecorded("omp", "working")
	if f1 := m.frameFile(); !m.Tick() || m.frameFile() == f1 {
		t.Errorf("working は tick ごとにコマが変わるはず: %s", f1)
	}
	m.Feed([]byte("\x1b"))
	if len(*got) != 1 || (*got)[0].kind != "ESCAPE" || m.mode != modeIdle {
		t.Errorf("working の ESC で idle: mode=%d", m.mode)
	}
	if m.Tick() {
		t.Error("idle は tick で描き直さない")
	}
}

func TestUnknownSequencesAreSkipped(t *testing.T) {
	m, _ := run3(t, "omp", "ask", "\x1b[1;5C", "\x1b[Z", down)
	if m.cursor != 3 {
		t.Errorf("未知の ESC シーケンスは読み捨てて続きを処理する: cursor=%d", m.cursor)
	}
}
