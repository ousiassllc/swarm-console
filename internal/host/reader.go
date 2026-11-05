package host

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

const (
	gib       = 1 << 30
	kib       = 1 << 10
	gibInKiB  = gib / kib
	defaultFS = "/proc"
)

// Reader は /proc・statfs・認証ファイルを読む Host の実装。複数の goroutine から同時に使える。
type Reader struct {
	clock  clock.Clock
	proc   string // /proc の場所（テストで差し替える）
	home   string // 認証ファイルの既定の場所の基準（"" なら os.UserHomeDir）
	statfs func(path string, st *syscall.Statfs_t) error

	cpuMu   sync.Mutex // CPU のサンプル（サンプラーだけが更新する）
	cpuPrev cpuCounters
	cpuHave bool
	cpu     CPU

	tblMu sync.Mutex // 親子表の更新を 1 つにまとめる（単一飛行）
	tbl   *procTable
	scans atomic.Int64 // /proc を走査した回数（テスト用）
}

var _ Host = (*Reader)(nil)

// NewReader は実機の /proc を読む Reader を作る。
func NewReader(c clock.Clock) *Reader {
	if c == nil {
		c = clock.Real{}
	}
	return &Reader{clock: c, proc: defaultFS, statfs: syscall.Statfs}
}

// Status は負荷・メモリ・スワップ・ディスク・稼働時間と最後の CPU サンプルを返す。メモリの単位は GiB（1024^3）。
// 読めない /proc ファイル（メモリ・スワップ・CPU を除く）はエラー。メモリ（/proc/meminfo）を読めない・値が不正なときはエラーにせず、
// MemTotalGB・MemFreeGB を 0 のまま残して稼働時間・CPU・スワップ・ディスクを返す（スワップと同じ扱い。P-95）。
// そのエラーは Status.MemErr に入れて返し（Reader にはロガーがないので記録は呼び出し側が行う）、Status のエラーにはしない。statfs に失敗したマウントは Available が false で残す。
func (r *Reader) Status(ctx context.Context, diskMounts []string) (Status, error) {
	var st Status
	if err := ctx.Err(); err != nil {
		return st, err
	}
	load, err := r.readLoad()
	if err != nil {
		return st, err
	}
	st.Load = load
	st.CPUs = runtime.NumCPU()
	if total, avail, merr := r.readMem(); merr != nil {
		st.MemErr = merr
	} else {
		st.MemTotalGB, st.MemFreeGB = total, avail
	}
	if st.Uptime, err = r.readUptime(); err != nil {
		return st, err
	}
	st.CPU = r.lastCPU()
	st.Swap = r.readSwap()
	for _, m := range diskMounts {
		var fs syscall.Statfs_t
		if err := r.statfs(m, &fs); err != nil {
			st.Disk = append(st.Disk, Disk{Mount: m}) // 残して取得できず（FR-REP-11）
			continue
		}
		st.Disk = append(st.Disk, diskOf(m, &fs))
	}
	return st, nil
}

func (r *Reader) readFile(name string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(r.proc, name)) //nolint:gosec // /proc 配下の固定名（呼び出しはリテラルだけ）
	if err != nil {
		return nil, fmt.Errorf("host: %s を読めません: %w", name, err)
	}
	return b, nil
}

func (r *Reader) readLoad() ([3]float64, error) {
	var out [3]float64
	b, err := r.readFile("loadavg")
	if err != nil {
		return out, err
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return out, fmt.Errorf("host: loadavg の形式が想定外")
	}
	for i := range out {
		if out[i], err = strconv.ParseFloat(f[i], 64); err != nil {
			return out, fmt.Errorf("host: loadavg の数値が不正: %w", err)
		}
	}
	return out, nil
}

// readMem は MemTotal と MemAvailable（kB）を GiB で返す。
func (r *Reader) readMem() (total, avail float64, err error) {
	b, err := r.readFile("meminfo")
	if err != nil {
		return 0, 0, err
	}
	var gotTotal, gotAvail bool
	for _, line := range bytes.Split(b, []byte("\n")) {
		key, rest, ok := strings.Cut(string(line), ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		kb, perr := strconv.ParseUint(f[0], 10, 64)
		if perr != nil {
			return 0, 0, fmt.Errorf("host: meminfo の数値が不正: %w", perr)
		}
		if key == "MemTotal" {
			total, gotTotal = float64(kb)/gibInKiB, true
		} else {
			avail, gotAvail = float64(kb)/gibInKiB, true
		}
	}
	if !gotTotal || !gotAvail {
		return 0, 0, fmt.Errorf("host: meminfo に MemTotal/MemAvailable がありません")
	}
	return total, avail, nil
}

func (r *Reader) readUptime() (time.Duration, error) {
	b, err := r.readFile("uptime")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 1 {
		return 0, fmt.Errorf("host: uptime の形式が想定外")
	}
	sec, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, fmt.Errorf("host: uptime の数値が不正: %w", err)
	}
	return time.Duration(sec * float64(time.Second)), nil
}
