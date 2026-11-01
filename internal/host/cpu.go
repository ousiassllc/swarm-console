package host

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"syscall"
)

// cpuCounters は /proc/stat の先頭 `cpu` 行の累積（jiffies）。
type cpuCounters struct {
	total, idle uint64
}

// SampleCPU は /proc/stat の先頭 `cpu` 行を読み、前回のサンプルとの差から CPU 使用率を更新する（report-values.md §3）。
// 最初のサンプル（差が無い）・読めない・合計が増えていないときは取得できず。
func (r *Reader) SampleCPU() {
	cur, err := r.readCPU()
	r.cpuMu.Lock()
	defer r.cpuMu.Unlock()
	if err != nil {
		r.cpuPrev, r.cpuHave, r.cpu = cpuCounters{}, false, CPU{}
		return
	}
	prev, have := r.cpuPrev, r.cpuHave
	r.cpuPrev, r.cpuHave = cur, true
	if !have || cur.total <= prev.total || cur.idle < prev.idle {
		r.cpu = CPU{}
		return
	}
	dTotal, dIdle := float64(cur.total-prev.total), float64(cur.idle-prev.idle)
	pct := int(math.Round(100 * (1 - dIdle/dTotal)))
	r.cpu = CPU{Available: true, UsedPercent: min(max(pct, 0), 100)}
}

func (r *Reader) lastCPU() CPU {
	r.cpuMu.Lock()
	defer r.cpuMu.Unlock()
	return r.cpu
}

// readCPU は `cpu  user nice system idle iowait irq softirq steal ...` を読む。idle は idle ＋ iowait、
// 合計は先頭 8 項目（guest は user に含まれるので足さない）。
func (r *Reader) readCPU() (cpuCounters, error) {
	b, err := r.readFile("stat")
	if err != nil {
		return cpuCounters{}, err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return cpuCounters{}, fmt.Errorf("host: stat の形式が想定外")
	}
	var c cpuCounters
	for i, s := range f[1:min(len(f), 9)] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return cpuCounters{}, fmt.Errorf("host: stat の数値が不正: %w", err)
		}
		c.total += v
		if i == 3 || i == 4 { // idle・iowait
			c.idle += v
		}
	}
	return c, nil
}

// readMeminfo は /proc/meminfo の「項目: 数値 kB」を map にする（数値にできない行は読み飛ばす）。
func (r *Reader) readMeminfo() (map[string]uint64, error) {
	b, err := r.readFile("meminfo")
	if err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		f := strings.Fields(rest)
		if !ok || len(f) == 0 {
			continue
		}
		if v, err := strconv.ParseUint(f[0], 10, 64); err == nil {
			out[key] = v
		}
	}
	return out, nil
}

// readSwap は SwapTotal・SwapFree（kB）から使用量を作る。読めない・項目が無いときは取得できず。
func (r *Reader) readSwap() Swap {
	kb, err := r.readMeminfo()
	if err != nil {
		return Swap{}
	}
	total, okT := kb["SwapTotal"]
	free, okF := kb["SwapFree"]
	if !okT || !okF || free > total {
		return Swap{}
	}
	if total == 0 {
		return Swap{Available: true} // スワップ無し（取得できずではない）
	}
	used := total - free
	return Swap{
		Available: true, TotalGB: round1(float64(total) / gibInKiB), UsedGB: round1(float64(used) / gibInKiB),
		UsedPercent: int(math.Round(100 * float64(used) / float64(total))),
	}
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// diskOf は statfs の結果から Disk を作る。used_percent は df の Use% と同じ（切り上げ）。
func diskOf(mount string, fs *syscall.Statfs_t) Disk {
	bsize := float64(fs.Bsize)
	used := float64(fs.Blocks - min(fs.Bfree, fs.Blocks)) // 使用 ＝ 全体 − 空き（root 予約も使用に数える）
	avail := float64(fs.Bavail)
	pct := 0
	if used+avail > 0 {
		pct = int(math.Ceil(100 * used / (used + avail)))
	}
	return Disk{
		Mount: mount, Available: true, FreeGB: avail * bsize / gib, TotalGB: float64(fs.Blocks) * bsize / gib,
		UsedPercent: min(pct, 100),
	}
}
