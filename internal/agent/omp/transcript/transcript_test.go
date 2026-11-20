package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func line(sec int, role, stop, content string) string {
	s := ""
	if stop != "" {
		s = `,"stopReason":"` + stop + `"`
	}
	return fmt.Sprintf(`{"type":"message","timestamp":%q,"message":{"role":%q%s,"content":%s}}`,
		time.Date(2026, 11, 20, 12, 0, sec, 0, time.UTC).Format(time.RFC3339Nano), role, s, content)
}

func text(s string) string { return fmt.Sprintf(`[{"type":"text","text":%q}]`, s) }

func askCall() string {
	return `[{"type":"toolCall","id":"c1","name":"ask","intent":"Asking","arguments":{"questions":[{"question":"Q?","options":["a","b"]}]}}]`
}

func readLines(t *testing.T, lines ...string) []Message {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, ok := Read(path)
	if !ok {
		t.Fatal("Read: ok=false")
	}
	return msgs
}

// 番の途中か：指示・ツールの結果・ツールの呼び出しは途中、最終の応答・中断・失敗は終わり。ほかの行（custom など）は数えない。
func TestInTurn(t *testing.T) {
	custom := `{"type":"custom","customType":"tool_execution_start"}`
	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"記録なし", nil, false},
		{"指示だけ", []string{line(1, "user", "", text("go"))}, true},
		{"ツールの呼び出し（結果待ち）", []string{line(1, "user", "", text("go")), line(2, "assistant", "toolUse", askCall()), custom}, true},
		{"ツールの結果", []string{line(1, "user", "", text("go")), line(2, "assistant", "toolUse", askCall()), line(3, "toolResult", "", text("x"))}, true},
		{"最終の応答", []string{line(1, "user", "", text("go")), line(2, "assistant", "stop", text("done"))}, false},
		{"中断", []string{line(1, "user", "", text("go")), line(2, "assistant", "aborted", text(""))}, false},
		{"失敗", []string{line(1, "user", "", text("go")), line(2, "assistant", "error", text(""))}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var msgs []Message
			if c.lines != nil {
				msgs = readLines(t, c.lines...)
			}
			if got := InTurn(msgs); got != c.want {
				t.Errorf("InTurn = %v, want %v", got, c.want)
			}
		})
	}
}

// 質問が出ているのは、最後のやり取りが ask の呼び出しのときだけ。回答（ツールの結果）が記録されたら出ていない。
func TestPendingAsk(t *testing.T) {
	msgs := readLines(t, line(1, "user", "", text("go")), line(2, "assistant", "toolUse", askCall()))
	call, ok := PendingAsk(msgs)
	if !ok || call.Name != "ask" || call.Intent != "Asking" {
		t.Fatalf("PendingAsk = %+v %v", call, ok)
	}
	if qs := ParseAsk(call.Args); len(qs) != 1 || qs[0].Question != "Q?" {
		t.Errorf("ParseAsk = %+v", qs)
	}
	answered := readLines(t, line(1, "user", "", text("go")), line(2, "assistant", "toolUse", askCall()), line(3, "toolResult", "", text("a")))
	if _, ok := PendingAsk(answered); ok {
		t.Error("回答が記録されたあとも質問が出ているとした")
	}
	other := readLines(t, line(1, "user", "", text("go")), line(2, "assistant", "toolUse", `[{"type":"toolCall","id":"c","name":"bash","arguments":{}}]`))
	if _, ok := PendingAsk(other); ok {
		t.Error("bash の呼び出しを質問とした")
	}
	if _, ok := PendingAsk(nil); ok {
		t.Error("記録なしで質問が出ているとした")
	}
}

func TestTurnStartAndLastCall(t *testing.T) {
	msgs := readLines(t,
		line(1, "user", "", text("first")), line(2, "assistant", "stop", text("ok")),
		line(10, "user", "", text("second")), line(11, "assistant", "toolUse", `[{"type":"toolCall","id":"c","name":"bash","intent":"Run it","arguments":{}}]`),
		line(12, "toolResult", "", text("out")))
	if got, want := TurnStart(msgs), time.Date(2026, 11, 20, 12, 0, 10, 0, time.UTC); !got.Equal(want) {
		t.Errorf("TurnStart = %v, want %v", got, want)
	}
	if c, ok := LastCall(msgs); !ok || c.Intent != "Run it" {
		t.Errorf("LastCall = %+v %v（結果のあとでも、直前の呼び出し）", c, ok)
	}
	if _, ok := LastCall(readLines(t, line(1, "user", "", text("x")))); ok {
		t.Error("呼び出しが無いのに LastCall が返った")
	}
}

// content が文字列の指示も読める。役が違う行（developer）と、読めない時刻の行は捨てる。
func TestReadSkipsUnusableLines(t *testing.T) {
	msgs := readLines(t,
		line(1, "user", "", `"plain text"`),
		line(2, "developer", "", text("hidden")),
		`{"type":"message","timestamp":"bad","message":{"role":"user","content":"x"}}`,
		`{"type":"message","timestamp":`,
		line(3, "assistant", "stop", text("done")))
	if len(msgs) != 2 || msgs[0].Text != "plain text" || msgs[1].Text != "done" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

// 末尾の大きな行（ツールの結果が数百 KB）があっても、その前のやり取りを読む。読み始めで切れた最初の行は捨てる。
func TestReadWidensForLargeToolResults(t *testing.T) {
	big := line(3, "toolResult", "", text(strings.Repeat("x", 300<<10)))
	msgs := readLines(t, line(1, "user", "", text("go")), line(2, "assistant", "toolUse", askCall()), big)
	if len(msgs) != 3 || msgs[0].Role != "user" || msgs[2].Role != "toolResult" {
		t.Fatalf("roles = %v", roles(msgs))
	}
}

func TestReadKeepsOnlyRecentMessages(t *testing.T) {
	var lines []string
	for i := range transcriptMessages + 10 {
		lines = append(lines, line(i%60, "user", "", text(fmt.Sprintf("m%d", i))))
	}
	msgs := readLines(t, lines...)
	if len(msgs) != transcriptMessages || msgs[len(msgs)-1].Text != fmt.Sprintf("m%d", transcriptMessages+9) {
		t.Fatalf("len=%d last=%q", len(msgs), msgs[len(msgs)-1].Text)
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, ok := Read(filepath.Join(t.TempDir(), "none.jsonl")); ok {
		t.Fatal("無いファイルで ok=true")
	}
}

func roles(msgs []Message) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.Role)
	}
	return out
}
