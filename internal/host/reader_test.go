package host

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fakeProc(t *testing.T) *Reader {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loadavg"), "1.50 2.25 3.00 2/900 12345\n")
	writeFile(t, filepath.Join(dir, "meminfo"), "MemTotal:       16777216 kB\nMemFree:          100 kB\nMemAvailable:    4194304 kB\nBuffers: 1 kB\n")
	writeFile(t, filepath.Join(dir, "uptime"), "3723.45 99999.00\n")
	r := NewReader(clock.Real{})
	r.proc = dir
	return r
}

func TestStatusParsesProcFiles(t *testing.T) {
	r := fakeProc(t)
	r.statfs = func(path string, st *syscall.Statfs_t) error {
		if path == "/bad" {
			return syscall.ENOENT
		}
		st.Bsize, st.Bavail = 4096, 2*gib/4096
		return nil
	}
	st, err := r.Status(context.Background(), []string{"/", "/bad", "/mnt/nvme"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Load != [3]float64{1.5, 2.25, 3} || st.MemTotalGB != 16 || st.MemFreeGB != 4 {
		t.Fatalf("%+v", st)
	}
	if st.Uptime != 3723450*time.Millisecond || st.CPUs < 1 {
		t.Fatalf("uptime/cpus: %+v", st)
	}
	// statfs に失敗したマウントは消さず、取得できず（FR-REP-11）。
	if len(st.Disk) != 3 || st.Disk[0].Mount != "/" || st.Disk[1].Mount != "/bad" || st.Disk[2].Mount != "/mnt/nvme" ||
		math.Abs(st.Disk[0].FreeGB-2) > 1e-9 || st.Disk[1].Available || !st.Disk[2].Available {
		t.Fatalf("disk: %+v", st.Disk)
	}
}

// meminfo を読めない・項目がない・数値が不正でも Status は失敗せず、メモリだけ 0 のまま、
// 負荷・稼働時間・スワップ・ディスクは残る（スワップと同じ扱い。P-95）。
func TestStatusMemFailureKeepsOtherValues(t *testing.T) {
	tests := []struct {
		name string
		body string // "" は meminfo を消す
	}{
		{"meminfo が無い", ""},
		{"MemAvailable がない", "MemTotal: 1 kB\nSwapTotal: 8388608 kB\nSwapFree: 7969178 kB\n"},
		{"MemTotal がない", "MemAvailable: 1 kB\nSwapTotal: 8388608 kB\nSwapFree: 7969178 kB\n"},
		{"数値が不正", "MemTotal: x kB\nMemAvailable: 1 kB\nSwapTotal: 8388608 kB\nSwapFree: 7969178 kB\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeProc(t)
			r.statfs = func(_ string, st *syscall.Statfs_t) error {
				st.Bsize, st.Bavail = 4096, 2*gib/4096
				return nil
			}
			path := filepath.Join(r.proc, "meminfo")
			if tt.body == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, path, tt.body)
			}
			st, err := r.Status(context.Background(), []string{"/"})
			if err != nil {
				t.Fatalf("メモリだけの失敗は Status の失敗にしない: %v", err)
			}
			if st.MemTotalGB != 0 || st.MemFreeGB != 0 {
				t.Fatalf("メモリは 0 のはず: %+v", st)
			}
			if st.Load != [3]float64{1.5, 2.25, 3} || st.Uptime != 3723450*time.Millisecond || st.CPUs < 1 {
				t.Fatalf("負荷・稼働時間・CPU 数は残るはず: %+v", st)
			}
			if len(st.Disk) != 1 || !st.Disk[0].Available {
				t.Fatalf("ディスクは残るはず: %+v", st.Disk)
			}
			wantSwap := tt.body != ""
			if wantSwap && !(st.Swap.Available && st.Swap.TotalGB == 8) {
				t.Fatalf("スワップは残るはず: %+v", st.Swap)
			}
		})
	}
}

func TestStatusErrors(t *testing.T) {
	tests := []struct{ name, file, body string }{
		{"loadavg が短い", "loadavg", "1.0\n"},
		{"loadavg が数値でない", "loadavg", "a b c\n"},
		{"uptime が空", "uptime", ""},
		{"uptime が数値でない", "uptime", "x y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeProc(t)
			writeFile(t, filepath.Join(r.proc, tt.file), tt.body)
			if _, err := r.Status(context.Background(), nil); err == nil {
				t.Fatal("エラーのはず")
			}
		})
	}
	r := fakeProc(t)
	if err := os.Remove(filepath.Join(r.proc, "uptime")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Status(context.Background(), nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ファイル無し: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fakeProc(t).Status(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("キャンセル: %v", err)
	}
}

// 実機の /proc と statfs で Status が通り、値が妥当な範囲にある。
func TestStatusReal(t *testing.T) {
	st, err := NewReader(clock.Real{}).Status(context.Background(), []string{"/"})
	if err != nil {
		t.Skipf("/proc を読めない環境: %v", err)
	}
	if st.CPUs < 1 || st.MemTotalGB <= 0 || st.MemFreeGB <= 0 || st.MemFreeGB > st.MemTotalGB || st.Uptime <= 0 || len(st.Disk) != 1 {
		t.Fatalf("%+v", st)
	}
}

func addProc(t *testing.T, root string, pid, ppid int, comm string, argv ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	writeFile(t, filepath.Join(dir, "stat"), strconv.Itoa(pid)+" ("+comm+") S "+strconv.Itoa(ppid)+" 1 1 0 -1 0\n")
	cmd := ""
	for _, a := range argv {
		cmd += a + "\x00"
	}
	writeFile(t, filepath.Join(dir, "cmdline"), cmd)
}

func TestProcTree(t *testing.T) {
	r := fakeProc(t)
	addProc(t, r.proc, 10, 1, "fish", "fish")
	addProc(t, r.proc, 11, 10, "node", "node", "/usr/bin/claude", "--flag value")
	addProc(t, r.proc, 12, 11, "a) S 1 (b", "sh", "-c", "") // comm に ")" と空白。空引数を保つ
	addProc(t, r.proc, 13, 10, "kthread")                   // cmdline が空
	addProc(t, r.proc, 20, 1, "other", "other")             // 別の木
	addProc(t, r.proc, 21, 20, "child", "child")
	writeFile(t, filepath.Join(r.proc, "self", "stat"), "garbage")
	writeFile(t, filepath.Join(r.proc, "99", "stat"), "broken") // 解析できない
	writeFile(t, filepath.Join(r.proc, "notapid"), "x")

	got, err := r.ProcTree(10)
	if err != nil {
		t.Fatal(err)
	}
	want := []Proc{
		{PID: 10, PPID: 1, Argv: []string{"fish"}},
		{PID: 11, PPID: 10, Argv: []string{"node", "/usr/bin/claude", "--flag value"}},
		{PID: 12, PPID: 11, Argv: []string{"sh", "-c", ""}},
		{PID: 13, PPID: 10},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.PID != w.PID || g.PPID != w.PPID || len(g.Argv) != len(w.Argv) {
			t.Fatalf("[%d] got %+v want %+v", i, g, w)
		}
		for j := range w.Argv {
			if g.Argv[j] != w.Argv[j] {
				t.Fatalf("[%d] argv %q want %q", i, g.Argv, w.Argv)
			}
		}
	}
	// 葉。
	if leaf, err := r.ProcTree(21); err != nil || len(leaf) != 1 || leaf[0].PID != 21 {
		t.Fatalf("leaf: %v %+v", err, leaf)
	}
	// 存在しない根。
	if _, err := r.ProcTree(555); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("根が無い: %v", err)
	}
}

func TestProcTreeRealSelf(t *testing.T) {
	got, err := NewReader(clock.Real{}).ProcTree(os.Getpid())
	if err != nil {
		t.Skipf("/proc を読めない環境: %v", err)
	}
	if len(got) == 0 || got[0].PID != os.Getpid() || got[0].PPID != os.Getppid() || len(got[0].Argv) == 0 {
		t.Fatalf("%+v", got)
	}
}
