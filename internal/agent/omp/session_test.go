package omp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Issue #118: omp が開いているセッションの記録（jsonl）の最後が、番の途中（ツールの実行中を含む）かを読む。

type sessRig struct {
	t    *testing.T
	proc string
	root string
	a    *adapter
	dir  string // root/<作業ディレクトリ>
}

func newSessRig(t *testing.T) *sessRig {
	t.Helper()
	base := t.TempDir()
	g := &sessRig{t: t, proc: filepath.Join(base, "proc"), root: filepath.Join(base, "sessions")}
	g.dir = filepath.Join(g.root, "--work-dir--")
	for _, d := range []string{g.proc, g.dir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	g.a = &adapter{procDir: g.proc, sessionsDir: g.root}
	return g
}

// open は pid が path を開いていることにする（/proc/<pid>/fd/<n> のリンク）。
func (g *sessRig) open(pid int, n int, path string) {
	g.t.Helper()
	fd := filepath.Join(g.proc, strconv.Itoa(pid), "fd")
	if err := os.MkdirAll(fd, 0o750); err != nil {
		g.t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(fd, strconv.Itoa(n))); err != nil {
		g.t.Fatal(err)
	}
}

func (g *sessRig) write(path string, lines ...string) {
	g.t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		g.t.Fatal(err)
	}
}

func msg(ts, role, stop string) string {
	s := ""
	if stop != "" {
		s = `,"stopReason":"` + stop + `"`
	}
	return fmt.Sprintf(`{"type":"message","id":"x","timestamp":%q,"message":{"role":%q%s,"content":[{"type":"text","text":"t"}]}}`, ts, role, s)
}

func ts(sec int) string {
	return time.Date(2026, 11, 20, 12, 0, sec, 0, time.UTC).Format(time.RFC3339Nano)
}

func at(sec int) time.Time { return time.Date(2026, 11, 20, 12, 0, sec, 0, time.UTC) }

func TestTurnReadsEndOfSession(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		inTurn bool
		last   time.Time
	}{
		{"ツールの実行中（呼び出しが最後）", []string{msg(ts(1), "user", ""), msg(ts(2), "assistant", "toolUse")}, true, at(2)},
		{"ツールの結果が最後（次の応答待ち）", []string{msg(ts(2), "assistant", "toolUse"), msg(ts(3), "toolResult", "")}, true, at(3)},
		{"指示を受けた直後", []string{msg(ts(4), "user", "")}, true, at(4)},
		{"最終の応答で終わった", []string{msg(ts(2), "assistant", "toolUse"), msg(ts(3), "toolResult", ""), msg(ts(5), "assistant", "stop")}, false, at(5)},
		{"中断で終わった", []string{msg(ts(2), "user", ""), msg(ts(6), "assistant", "aborted")}, false, at(6)},
		{"失敗で終わった", []string{msg(ts(2), "user", ""), msg(ts(7), "assistant", "error")}, false, at(7)},
		{"番の判定に使わない行（custom・developer・model_usage）は飛ばす", []string{
			msg(ts(2), "assistant", "toolUse"),
			`{"type":"custom","customType":"tool_execution_start","timestamp":"` + ts(9) + `"}`,
			msg(ts(9), "developer", ""),
			`{"type":"model_usage","timestamp":"` + ts(9) + `"}`,
		}, true, at(2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newSessRig(t)
			f := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_abc.jsonl")
			g.write(f, tc.lines...)
			g.open(4242, 81, f)
			last, in, ok := g.a.Turn(4242, time.Time{})
			if !ok || in != tc.inTurn || !last.Equal(tc.last) {
				t.Fatalf("Turn = (%v, %v, %v), want (%v, %v, true)", last, in, ok, tc.last, tc.inTurn)
			}
		})
	}
}

// since より前のやり取りだけなら、作業の跡とは数えない（ok=false）。
func TestTurnIgnoresInteractionsBeforeSince(t *testing.T) {
	g := newSessRig(t)
	f := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_abc.jsonl")
	g.write(f, msg(ts(2), "assistant", "toolUse"))
	g.open(4242, 81, f)
	if _, _, ok := g.a.Turn(4242, at(3)); ok {
		t.Fatal("since より前のやり取りを数えた")
	}
	if _, _, ok := g.a.Turn(4242, at(2)); !ok {
		t.Fatal("since ちょうどのやり取りを数えない")
	}
}

// 助言役（__advisor.jsonl）・サブエージェントの記録（セッションの名前のフォルダの中）・sessions の外のファイルは、会話の記録として読まない。
func TestTurnReadsOnlyTheMainSessionFile(t *testing.T) {
	g := newSessRig(t)
	main := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_abc.jsonl")
	g.write(main, msg(ts(1), "user", ""), msg(ts(2), "assistant", "stop"))
	sub := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_abc")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	adv := filepath.Join(sub, "__advisor.jsonl")
	g.write(adv, msg(ts(30), "assistant", "toolUse"))
	outside := filepath.Join(t.TempDir(), "other.jsonl")
	g.write(outside, msg(ts(40), "assistant", "toolUse"))
	g.open(7, 10, adv)
	g.open(7, 11, outside)
	g.open(7, 12, main)
	last, in, ok := g.a.Turn(7, time.Time{})
	if !ok || in || !last.Equal(at(2)) {
		t.Fatalf("Turn = (%v, %v, %v), want main の最終の応答 (%v, false, true)", last, in, ok, at(2))
	}
}

// 開いている記録が無い・pid が違う・読めない記録は ok=false（画面どおり。誤って working にしない）。
func TestTurnWithoutSessionFile(t *testing.T) {
	g := newSessRig(t)
	if _, _, ok := g.a.Turn(1, time.Time{}); ok {
		t.Fatal("/proc/<pid>/fd が無いのに ok")
	}
	f := filepath.Join(g.dir, "gone.jsonl")
	g.open(2, 3, f) // 指す先が無い
	if _, _, ok := g.a.Turn(2, time.Time{}); ok {
		t.Fatal("消えた記録で ok")
	}
	g.write(f, "not json", "{")
	if _, _, ok := g.a.Turn(2, time.Time{}); ok {
		t.Fatal("読めない記録で ok")
	}
}

// 1 行が数十〜百 KB のツールの結果が末尾にあっても、その前のやり取りまで読める。末尾が大きくて途中で切れる場合も、行の頭から読み直す。
func TestTurnReadsPastLargeToolResults(t *testing.T) {
	g := newSessRig(t)
	f := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_abc.jsonl")
	big := `{"type":"message","id":"b","timestamp":"` + ts(3) + `","message":{"role":"toolResult","content":[{"type":"text","text":"` + strings.Repeat("x", 300<<10) + `"}]}}`
	g.write(f, msg(ts(1), "user", ""), msg(ts(2), "assistant", "toolUse"), big)
	g.open(5, 9, f)
	last, in, ok := g.a.Turn(5, time.Time{})
	if !ok || !in || !last.Equal(at(3)) {
		t.Fatalf("大きな結果が最後: Turn = (%v, %v, %v)", last, in, ok)
	}
	// 大きな行の前の最後の assistant だけが番の終わりを決める場合（大きな行が developer などで、判定に使わない）。
	bigDev := `{"type":"message","id":"b","timestamp":"` + ts(4) + `","message":{"role":"developer","content":"` + strings.Repeat("y", 300<<10) + `"}}`
	g.write(f, msg(ts(1), "user", ""), msg(ts(2), "assistant", "stop"), bigDev)
	last, in, ok = g.a.Turn(5, time.Time{})
	if !ok || in || !last.Equal(at(2)) {
		t.Fatalf("大きな行の前の最終の応答: Turn = (%v, %v, %v)", last, in, ok)
	}
}
