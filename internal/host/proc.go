package host

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// ProcTree は pid を根とするプロセス木（pid 自身を含む）を PID 昇順で返す。
// /proc/<pid>/stat の PPID から子孫を集め、/proc/<pid>/cmdline を NUL で分けて Argv にする。
// 根が存在しなければ os.ErrNotExist を包んだエラー。走査中に消えた子孫・cmdline が読めないプロセスは
// Argv を空にして（または除いて）続ける。
func (r *Reader) ProcTree(pid int) ([]Proc, error) {
	entries, err := os.ReadDir(r.proc)
	if err != nil {
		return nil, fmt.Errorf("host: /proc を読めません: %w", err)
	}
	children := make(map[int][]int)
	exists := false
	for _, e := range entries {
		p, err := strconv.Atoi(e.Name())
		if err != nil || p <= 0 {
			continue
		}
		ppid, ok := r.readPPID(p)
		if !ok {
			continue
		}
		if p == pid {
			exists = true
		}
		children[ppid] = append(children[ppid], p)
	}
	if !exists {
		return nil, fmt.Errorf("host: pid %d: %w", pid, os.ErrNotExist)
	}

	seen := map[int]bool{pid: true}
	queue := []int{pid}
	var out []Proc
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, Proc{PID: cur, Argv: r.readArgv(cur)})
		for _, c := range children[cur] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
	}
	// PPID を埋める。
	parent := make(map[int]int, len(out))
	for pp, cs := range children {
		for _, c := range cs {
			parent[c] = pp
		}
	}
	for i := range out {
		out[i].PPID = parent[out[i].PID]
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
