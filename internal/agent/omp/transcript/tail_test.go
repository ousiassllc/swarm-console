package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTail(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path) //nolint:gosec // テストの一時ファイル
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestReadTail(t *testing.T) {
	const content = "aaaa\nbbbb\ncccc\n" // 15 バイト。行は 0〜4・5〜9・10〜14（末尾の改行が 4・9・14）
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{"ファイルより大きい窓は全体", 100, content},
		{"窓がちょうどファイル全体", 15, content},
		{"先頭の行の途中から読むと切れた行を捨てる", 14, "bbbb\ncccc\n"},
		{"窓が前の行の改行から始まれば、空の行を捨てて次の行から", 11, "bbbb\ncccc\n"},
		{"窓が行頭から始まっても先頭の 1 行は捨てる", 10, "cccc\n"},
		{"窓が最後の行より小さければ空", 3, ""},
		{"窓が最後の改行だけ", 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := writeTail(t, content)
			got, err := ReadTail(f, int64(len(content)), tt.n)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("ReadTail(n=%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

type tailCall struct {
	buf     string
	atStart bool
}

func collectGrow(t *testing.T, content string, minSize, maxSize int64, doneAt int) []tailCall {
	t.Helper()
	f := writeTail(t, content)
	var calls []tailCall
	err := GrowTail(f, minSize, maxSize, func(buf []byte, atStart bool) bool {
		calls = append(calls, tailCall{string(buf), atStart})
		return len(calls) == doneAt
	})
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

func TestGrowTail(t *testing.T) {
	t.Run("ファイルが最小の窓より小さければ 1 回だけ全体を渡す", func(t *testing.T) {
		calls := collectGrow(t, "aaaa\nbbbb\n", 64, 1024, 0)
		if len(calls) != 1 || calls[0] != (tailCall{"aaaa\nbbbb\n", true}) {
			t.Errorf("calls = %+v", calls)
		}
	})
	t.Run("done なら窓を広げない", func(t *testing.T) {
		calls := collectGrow(t, strings.Repeat("aaaa\n", 100), 10, 1024, 1)
		if len(calls) != 1 || calls[0].atStart || calls[0].buf != "aaaa\n" {
			t.Errorf("calls = %+v", calls)
		}
	})
	t.Run("先頭の行が切れる位置では切れた行を捨て、倍に広げて再び呼ぶ", func(t *testing.T) {
		// 行は 0〜4・5〜9・10〜14・15〜19（20 バイト）。
		calls := collectGrow(t, "aaaa\nbbbb\ncccc\ndddd\n", 7, 1024, 0)
		want := []tailCall{
			{"dddd\n", false},                  // n=7: 13 から "cc\ndddd\n"。切れた "cc" を捨てる
			{"cccc\ndddd\n", false},            // n=14: 6 から "bbb\ncccc\ndddd\n"。切れた "bbb" を捨てる
			{"aaaa\nbbbb\ncccc\ndddd\n", true}, // n=28 はファイルより大きい: 先頭まで
		}
		if len(calls) != len(want) {
			t.Fatalf("calls = %+v, want %+v", calls, want)
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Errorf("calls[%d] = %+v, want %+v", i, calls[i], want[i])
			}
		}
	})
	t.Run("1 行が最小の窓より大きければ空を渡して広げ、先頭まで届いたら全体を渡す", func(t *testing.T) {
		big := strings.Repeat("x", 100)
		content := "head\n" + big + "\n" // 106 バイト
		calls := collectGrow(t, content, 16, 1024, 0)
		// n=16・32・64 は 1 行の途中で、切れた行を捨てると空。n=128 でファイルの先頭に届く。
		want := []tailCall{{"", false}, {"", false}, {"", false}, {content, true}}
		if len(calls) != len(want) {
			t.Fatalf("calls = %+v, want %+v", calls, want)
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Errorf("calls[%d] = %+v, want %+v", i, calls[i], want[i])
			}
		}
	})
	t.Run("窓が長い行をちょうど含めば、その行だけを渡す", func(t *testing.T) {
		big := strings.Repeat("x", 100)
		content := "head\n" + big + "\n"
		calls := collectGrow(t, content, 102, 1024, 1)
		if len(calls) != 1 || calls[0] != (tailCall{big + "\n", false}) {
			t.Errorf("calls = %+v", calls)
		}
	})
	t.Run("窓が上限に達したら done でなくても止まる", func(t *testing.T) {
		big := strings.Repeat("x", 100)
		calls := collectGrow(t, "head\n"+big+"\n", 16, 32, 0)
		if len(calls) != 2 || calls[0] != (tailCall{"", false}) || calls[1] != (tailCall{"", false}) {
			t.Errorf("calls = %+v, want 16・32 の 2 回とも空", calls)
		}
	})
}

// 末尾読みの窓の下限・上限は、session.go・reaction.go・Read が同じ定数を使う（Issue #149）。値を変えるときはここも意識して変える。
func TestTailBounds(t *testing.T) {
	if TailMin != 256<<10 {
		t.Errorf("TailMin = %d, want %d（256 KiB）", TailMin, 256<<10)
	}
	if TailMax != 2<<20 {
		t.Errorf("TailMax = %d, want %d（2 MiB）", TailMax, 2<<20)
	}
}
