package panetest

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
)

// FakeHost は host.Host のフェイク。
type FakeHost struct {
	mu      sync.Mutex
	trees   map[int][]host.Proc
	samples int
	Load    float64
	// StatusErr が非 nil なら Status はこのエラーを返す（/proc が読めない状態）。
	StatusErr error
	// MemErr が非 nil なら Status は成功のまま Status.MemErr にこれを入れる（/proc/meminfo が読めない状態）。
	MemErr error
}

// NewHost は空のフェイクを作る。プロセス木が無い pid は ProcTree がエラーを返す（/proc が読めない状態）。
func NewHost() *FakeHost { return &FakeHost{trees: map[int][]host.Proc{}} }

// SampleCPU は呼ばれた回数を数える（サンプラーだけが呼ぶことの確認用）。
func (h *FakeHost) SampleCPU() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.samples++
}

// Samples は SampleCPU が呼ばれた回数。
func (h *FakeHost) Samples() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.samples
}

// SetTree は pid のプロセス木を設定する。
func (h *FakeHost) SetTree(pid int, argvs ...[]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var ps []host.Proc
	for i, a := range argvs {
		ps = append(ps, host.Proc{PID: pid + i, PPID: pid, Argv: a})
	}
	h.trees[pid] = ps
}

// Status は固定値。
func (h *FakeHost) Status(context.Context, []string) (host.Status, error) {
	if h.StatusErr != nil {
		return host.Status{}, h.StatusErr
	}
	return host.Status{Load: [3]float64{h.Load, 0, 0}, CPUs: 4, MemErr: h.MemErr}, nil
}

// ClearTree は pid のプロセス木を消す（以後 ProcTree はエラーを返す。/proc が読めない状態）。
func (h *FakeHost) ClearTree(pid int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.trees, pid)
}

// ProcTree は設定された木。
func (h *FakeHost) ProcTree(pid int) ([]host.Proc, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.trees[pid]
	if !ok {
		return nil, errors.New("プロセス木なし")
	}
	return t, nil
}

// AuthStatus は常に unknown。
func (h *FakeHost) AuthStatus(agent, _ string, _ time.Time) host.Auth {
	return host.Auth{Agent: agent, Status: host.AuthUnknown}
}

// ArchiveCall は FakeNotes.Archive の呼び出し。
type ArchiveCall struct {
	Meta     notes.ArchiveMeta
	ClosedBy string
}

// FakeNotes は pane.NoteStore のフェイク。
type FakeNotes struct {
	mu        sync.Mutex
	sums      map[string]notes.Summary
	archives  []ArchiveCall
	logs      []string
	reconcile []string
	LogErr    error
}

// NewNotes は空のフェイクを作る。
func NewNotes() *FakeNotes { return &FakeNotes{sums: map[string]notes.Summary{}} }

// SetSummary は id のメモ要約を設定する。
func (n *FakeNotes) SetSummary(id string, s notes.Summary) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sums[id] = s
}

// Summaries は設定された要約のコピー。
func (n *FakeNotes) Summaries() map[string]notes.Summary {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]notes.Summary, len(n.sums))
	for k, v := range n.sums {
		out[k] = v
	}
	return out
}

// Archive は記録し、メモがあれば（要約が設定されていれば）true を返す。
func (n *FakeNotes) Archive(_ context.Context, meta notes.ArchiveMeta, closedBy string) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.archives = append(n.archives, ArchiveCall{meta, closedBy})
	_, had := n.sums[meta.PaneID]
	delete(n.sums, meta.PaneID)
	return had, nil
}

// Log は記録する。
func (n *FakeNotes) Log(_ context.Context, paneID, text string) (int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.LogErr != nil {
		return 0, n.LogErr
	}
	n.logs = append(n.logs, paneID+": "+text)
	return len(n.logs), nil
}

// ReconcileServer は記録する。
func (n *FakeNotes) ReconcileServer(_ context.Context, start string) (int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.reconcile = append(n.reconcile, start)
	return 0, nil
}

// Archives は Archive の呼び出しの記録。
func (n *FakeNotes) Archives() []ArchiveCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]ArchiveCall(nil), n.archives...)
}

// Logs は Log の呼び出しの記録（"<id>: <text>"）。
func (n *FakeNotes) Logs() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.logs...)
}

// Reconciles は ReconcileServer に渡された起動時刻。
func (n *FakeNotes) Reconciles() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.reconcile...)
}

// MemFS はメモリ上の pane.ReportFS。
type MemFS struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]os.FileMode
}

// NewMemFS は空のファイルシステムを作る。
func NewMemFS() *MemFS { return &MemFS{files: map[string][]byte{}, dirs: map[string]os.FileMode{}} }

// Put はファイルを置く（空でもよい）。
func (m *MemFS) Put(p string, data string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[p] = []byte(data)
}

// Has はファイルがあるか。
func (m *MemFS) Has(p string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[p]
	return ok
}

// Files はファイルのパス一覧（昇順）。
func (m *MemFS) Files() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for p := range m.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// DirMode は MkdirAll で作ったディレクトリの権限。
func (m *MemFS) DirMode(p string) (os.FileMode, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	md, ok := m.dirs[p]
	return md, ok
}

type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o600 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// Lstat はファイルの情報（ディレクトリは対象外。リンクは持たない）。
func (m *MemFS) Lstat(p string) (fs.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.files[p]
	if !ok {
		return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
	}
	return memInfo{path.Base(p), int64(len(d))}, nil
}

// ReadFile は内容。
func (m *MemFS) ReadFile(p string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.files[p]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return d, nil
}

// MkdirAll はディレクトリを記録する。
func (m *MemFS) MkdirAll(p string, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[p] = perm
	return nil
}

// Rename はファイルを移す。
func (m *MemFS) Rename(from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.files[from]
	if !ok {
		return &fs.PathError{Op: "rename", Path: from, Err: fs.ErrNotExist}
	}
	delete(m.files, from)
	m.files[to] = d
	return nil
}
