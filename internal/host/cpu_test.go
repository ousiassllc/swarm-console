package host

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
)

func statLine(user, idle, iowait uint64) string {
	return "cpu  " + u(user) + " 0 0 " + u(idle) + " " + u(iowait) + " 0 0 0 0 0\ncpu0 1 1 1 1 1 0 0 0 0 0\nintr 1\n"
}

func u(v uint64) string { return fmtUint(v) }

func fmtUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for ; v > 0; v /= 10 {
		b = append([]byte{byte('0' + v%10)}, b...)
	}
	return string(b)
}

func cpuNow(t *testing.T, r *Reader) CPU {
	t.Helper()
	st, err := r.Status(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return st.CPU
}

// CPU は前回のサンプルとの差。1 回目（差が無い）は取得できず、2 回目で値が出る。
// Status は最後のサンプルを返すだけで、呼ぶたびに値が変わらない（FR-REP-7・report-values.md §3）。
func TestCPUSampling(t *testing.T) {
	r := fakeProc(t)
	stat := filepath.Join(r.proc, "stat")
	if cpuNow(t, r).Available {
		t.Fatal("サンプル前は取得できず")
	}
	writeFile(t, stat, statLine(100, 800, 100)) // 合計 1000
	r.SampleCPU()
	if cpuNow(t, r).Available {
		t.Fatal("1 回目は差が無いので取得できず")
	}
	writeFile(t, stat, statLine(400, 1500, 100)) // 差: 合計 1000・idle 700（iowait 差 0）→ 使用 30%
	r.SampleCPU()
	if c := cpuNow(t, r); !c.Available || c.UsedPercent != 30 {
		t.Fatalf("2 回目 = %+v, want 30%%", c)
	}
	// ファイルが進んでも、次のサンプルまで Status の値は変わらない。
	writeFile(t, stat, statLine(900, 1500, 100))
	if c := cpuNow(t, r); c.UsedPercent != 30 {
		t.Fatalf("サンプルの間で値が変わった: %+v", c)
	}
	// カウンターが戻ったら（再起動など）取得できず。
	writeFile(t, stat, statLine(100, 800, 100))
	r.SampleCPU()
	if c := cpuNow(t, r); c.Available {
		t.Fatalf("カウンターが戻ったら取得できず: %+v", c)
	}
	// iowait は idle に数える。前回(100,800,100)との差: user 100・idle 100・iowait 800 → 合計 1000・idle 900 → 10%。
	writeFile(t, stat, statLine(200, 900, 900))
	r.SampleCPU()
	if c := cpuNow(t, r); !c.Available || c.UsedPercent != 10 {
		t.Fatalf("iowait は idle: %+v", c)
	}
}

// 合計が増えていない・読めない・形式が違うときは取得できず（前の値を残さない）。
func TestCPUUnavailable(t *testing.T) {
	r := fakeProc(t)
	stat := filepath.Join(r.proc, "stat")
	writeFile(t, stat, statLine(100, 800, 100))
	r.SampleCPU()
	r.SampleCPU() // 合計が増えていない
	if cpuNow(t, r).Available {
		t.Fatal("合計が増えていなければ取得できず")
	}
	writeFile(t, stat, statLine(200, 900, 100))
	r.SampleCPU()
	if !cpuNow(t, r).Available {
		t.Fatal("増えたら取れる")
	}
	for _, body := range []string{"", "intr 1\n", "cpu  a b c d e\n"} {
		writeFile(t, stat, body)
		r.SampleCPU()
		if cpuNow(t, r).Available {
			t.Fatalf("%q は取得できず", body)
		}
	}
}

func TestSwap(t *testing.T) {
	tests := []struct {
		name string
		mem  string
		want Swap
	}{
		{"使用中", "MemTotal: 1 kB\nMemAvailable: 1 kB\nSwapTotal: 8388608 kB\nSwapFree: 7969178 kB\n", Swap{Available: true, TotalGB: 8, UsedGB: 0.4, UsedPercent: 5}},
		{"スワップ無し（取得できずではない）", "MemTotal: 1 kB\nMemAvailable: 1 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n", Swap{Available: true}},
		{"項目なし", "MemTotal: 1 kB\nMemAvailable: 1 kB\n", Swap{}},
		{"空きが全体より大きい", "MemTotal: 1 kB\nMemAvailable: 1 kB\nSwapTotal: 1 kB\nSwapFree: 2 kB\n", Swap{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeProc(t)
			writeFile(t, filepath.Join(r.proc, "meminfo"), tt.mem)
			st, err := r.Status(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if st.Swap != tt.want {
				t.Fatalf("swap = %+v, want %+v", st.Swap, tt.want)
			}
		})
	}
}

// used_percent は df の Use%（使用 ÷（使用 ＋ 一般ユーザーが使える空き）、切り上げ）。
func TestDiskUsedPercent(t *testing.T) {
	tests := []struct {
		name                 string
		blocks, bfree, avail uint64
		want                 int
	}{
		{"半分", 1000, 500, 500, 50},
		{"root 予約あり（空き 100 のうち一般は 50）", 1000, 100, 50, 95}, // 使用 900 ÷ (900+50)=94.7 → 95
		{"切り上げ", 1000, 999, 999, 1},                        // 使用 1 ÷ 1000 = 0.1 → 1
		{"使用なし", 1000, 1000, 1000, 0},
		{"満杯", 1000, 0, 0, 100},
		{"ブロックなし", 0, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &syscall.Statfs_t{Bsize: 4096, Blocks: tt.blocks, Bfree: tt.bfree, Bavail: tt.avail}
			d := diskOf("/x", fs)
			if !d.Available || d.UsedPercent != tt.want {
				t.Fatalf("%+v, want %d%%", d, tt.want)
			}
		})
	}
	d := diskOf("/x", &syscall.Statfs_t{Bsize: 1 << 30, Blocks: 450, Bfree: 120, Bavail: 120})
	if d.TotalGB != 450 || d.FreeGB != 120 {
		t.Fatalf("GiB: %+v", d)
	}
}
