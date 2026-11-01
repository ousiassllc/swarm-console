package toolstest

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/safety"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// Fixture はテストで使う一式。ペインは次の 5 つ（位置: ID, 状態）。
//
//	T1: %1 omp idle / T2: %2 omp asking / B1: %3 エージェントなし shell / B2: %4 claude working / B3: %5 エージェントなし unknown
type Fixture struct {
	Cfg      *config.Config
	Registry *Registry
	Ops      *Ops
	Notes    notes.Store
	Host     *Host
	Update   *Update
	Clock    *clock.Fake
	Root     string // allowed_roots・file_roots の根（一時ディレクトリ）
	Tools    *tools.Registry
	Deps     tools.Deps
	Logger   *slog.Logger
}

// At はスナップショットの時刻。
var At = time.Date(2026, 10, 31, 22, 10, 5, 0, time.FixedZone("JST", 9*3600))

// New は一時ディレクトリを使う Fixture を組み立てる。notes・safety・render は本物を使う。
func New(t *testing.T) *Fixture {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	cfg := &config.Config{
		Sessions: []string{"0"}, DefaultSession: "0", DefaultLang: "ja",
		AllowedRoots: []string{root}, FileRoots: []string{root}, ReportDir: filepath.Join(root, "reports"),
		TitlePrefix: "agent: ",
	}
	if err := os.MkdirAll(cfg.ReportDir, 0o700); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(At)
	store, err := notes.NewStore(filepath.Join(t.TempDir(), "notes.json"), clk, notes.WithSecretDetector(safety.DetectSecret))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := render.LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	f := &Fixture{
		Cfg: cfg, Registry: &Registry{Snap: DefaultSnapshot(), DefaultTail: pane.Tail{Text: "line1\nline2"}},
		Ops: &Ops{}, Notes: store, Host: &Host{St: DefaultHostStatus()}, Clock: clk, Root: root,
		Update: &Update{Info: DefaultUpdateInfo()},
	}
	f.Deps = tools.Deps{
		Cfg: cfg, Registry: f.Registry, Ops: f.Ops, Notes: store, Safety: safety.NewPolicy(cfg), Host: f.Host,
		Profiles: DefaultProfiles(), Templates: tpl, Update: f.Update, Clock: clk, Logger: slog.New(slog.DiscardHandler),
	}
	if f.Tools, err = tools.New(f.Deps); err != nil {
		t.Fatal(err)
	}
	return f
}

// DefaultUpdateInfo は host_status.update の固定値（確認前）。
func DefaultUpdateInfo() tools.UpdateInfo {
	return tools.UpdateInfo{Status: "pending", Current: "v0.1.4", Apply: tools.ApplyInfo{State: "idle"}}
}

// DefaultProfiles は fix プロファイルを持つ profiles。
func DefaultProfiles() *config.Profiles {
	return &config.Profiles{
		Common:   []string{"報告は {report_path} に書く"},
		Profiles: map[string]config.Profile{"fix": {Name: "fix", Rules: []string{"小さく直す"}}},
	}
}

// DefaultHostStatus は host_status の固定値。
func DefaultHostStatus() host.Status {
	return host.Status{
		Load: [3]float64{98.24, 95.1, 90.449}, CPUs: 32, MemTotalGB: 128, MemFreeGB: 41.26,
		Disk:   []host.Disk{{Mount: "/", FreeGB: 120}, {Mount: "/mnt/nvme", FreeGB: 830.04}},
		Uptime: 12*24*time.Hour + 4*time.Hour + 30*time.Minute,
	}
}

// DefaultSnapshot は Fixture の 5 ペインのスナップショット。
func DefaultSnapshot() *pane.Snapshot {
	mk := func(id, pos, ag string, st agent.State) pane.PaneState {
		return pane.PaneState{ID: id, Pos: pos, Session: "0", Agent: ag, State: st, Title: "agent: " + pos, Cwd: "/mnt/nvme/x"}
	}
	return &pane.Snapshot{
		At: At, Host: DefaultHostStatus(), Layout: pane.Layout{Rows: 2, Cols: []int{3, 2}}, TmuxSession: true,
		Panes: []pane.PaneState{
			mk("%1", "T1", "omp", agent.StateIdle),
			mk("%2", "T2", "omp", agent.StateAsking),
			mk("%3", "B1", "", agent.StateShell),
			mk("%4", "B2", "claude", agent.StateWorking),
			mk("%5", "B3", "", agent.StateUnknown),
		},
	}
}
