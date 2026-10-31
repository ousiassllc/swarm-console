package host

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// procTTL は親子表を使い回す時間。1 回のスナップショット（全ペイン分の ProcTree）の間は /proc を 1 回だけ走査する。
const procTTL = time.Second

// procTable は /proc 全体の親子表。作ったあと変更しない。
type procTable struct {
	gen      uint64
	at       time.Time
	parent   map[int]int
	children map[int][]int
}

// table は TTL 内なら共有の親子表を返し、古い（または無い）ときだけ走査する（単一飛行）。
// force が true のときは、seenGen の表を見たあとに他の goroutine が作り直していればそれを使い、
// そうでなければ TTL にかかわらず走査し直す。
func (r *Reader) table(force bool, seenGen uint64) (*procTable, error) {
	r.tblMu.Lock()
	defer r.tblMu.Unlock()
	t := r.tbl
	if t != nil {
		fresh := r.clock.Now().Sub(t.at) < procTTL
		if (!force && fresh) || (force && t.gen != seenGen) {
			return t, nil
		}
	}
	nt, err := r.scanProc()
	if err != nil {
		return nil, err
	}
	nt.at = r.clock.Now()
	if t != nil {
		nt.gen = t.gen + 1
	}
	r.tbl = nt
	return nt, nil
}

// scanProc は /proc/<pid>/stat を全部読んで親子表を作る。cmdline は読まない。
func (r *Reader) scanProc() (*procTable, error) {
	r.scans.Add(1)
	entries, err := os.ReadDir(r.proc)
	if err != nil {
		return nil, fmt.Errorf("host: /proc を読めません: %w", err)
	}
	t := &procTable{parent: make(map[int]int, len(entries)), children: make(map[int][]int)}
	for _, e := range entries {
		p, err := strconv.Atoi(e.Name())
		if err != nil || p <= 0 {
			continue
		}
		ppid, ok := r.readPPID(p)
		if !ok {
			continue
		}
		t.parent[p] = ppid
		t.children[ppid] = append(t.children[ppid], p)
	}
	return t, nil
}

// ProcTree は pid を根とするプロセス木（pid 自身を含む）を PID 昇順で返す。
// 親子関係は /proc を走査した共有の表（TTL 1 秒）から求め、表に載った子孫だけ /proc/<pid>/cmdline を NUL で分けて Argv にする。
// pid が表に無いときは古い表の見落としかもしれないので、1 回だけ強制的に走査し直す。
// それでも無ければ os.ErrNotExist を包んだエラー。消えたプロセス・cmdline が読めないプロセスは Argv を空にして続ける。
func (r *Reader) ProcTree(pid int) ([]Proc, error) {
	t, err := r.table(false, 0)
	if err != nil {
		return nil, err
	}
	if _, ok := t.parent[pid]; !ok {
		if t, err = r.table(true, t.gen); err != nil {
			return nil, err
		}
		if _, ok := t.parent[pid]; !ok {
			return nil, fmt.Errorf("host: pid %d: %w", pid, os.ErrNotExist)
		}
	}
	seen := map[int]bool{pid: true}
	queue := []int{pid}
	var out []Proc
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, Proc{PID: cur, PPID: t.parent[cur], Argv: r.readArgv(cur)})
		for _, c := range t.children[cur] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

// readPPID は /proc/<pid>/stat から PPID を読む。comm は空白や ")" を含みうるので、最後の ")" の後ろを使う。
func (r *Reader) readPPID(pid int) (int, bool) {
	b, err := os.ReadFile(filepath.Join(r.proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, false
	}
	f := bytes.Fields(b[i+1:]) // state ppid ...
	if len(f) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(string(f[1]))
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// readArgv は /proc/<pid>/cmdline を NUL で分ける。読めない・空（カーネルスレッド）なら nil。
func (r *Reader) readArgv(pid int) []string {
	b, err := os.ReadFile(filepath.Join(r.proc, strconv.Itoa(pid), "cmdline"))
	if err != nil || len(b) == 0 {
		return nil
	}
	b = bytes.TrimSuffix(b, []byte{0})
	parts := bytes.Split(b, []byte{0})
	argv := make([]string, len(parts))
	for i, p := range parts {
		argv[i] = string(p)
	}
	return argv
}
