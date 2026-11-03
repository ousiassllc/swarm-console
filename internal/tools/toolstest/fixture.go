package toolstest

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
	"github.com/ousiassllc/swarm-console/internal/safety"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// Fixture はテストで使う一式。セッション 0 のペインは次の 5 つ（位置: ID, 状態。上段 2・下段 3）。
//
//	T1: %1 omp idle / T2: %2 omp asking / B1: %3 エージェントなし shell / B2: %4 claude working / B3: %5 エージェントなし unknown
type Fixture struct {
	Cfg      *config.Config
	Registry *Registry
	Ops      *Ops
	Notes    notes.Store
	Host     *Host
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
		TitlePrefix: "agent: ", FileGetMaxBytes: config.DefaultFileGetMaxBytes,
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
	}
	f.Deps = tools.Deps{
		Cfg: cfg, Registry: f.Registry, Ops: f.Ops, Notes: store, Safety: safety.NewPolicy(cfg), Host: f.Host,
		Profiles: DefaultProfiles(), Templates: tpl, Clock: clk, Logger: slog.New(slog.DiscardHandler),
	}
	if f.Tools, err = tools.New(f.Deps); err != nil {
		t.Fatal(err)
	}
	return f
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
		CPU:  host.CPU{Available: true, UsedPercent: 42},
		Swap: host.Swap{Available: true, TotalGB: 8, UsedGB: 0.4, UsedPercent: 5},
		Disk: []host.Disk{
			{Mount: "/", Available: true, FreeGB: 120, TotalGB: 450, UsedPercent: 73},
			{Mount: "/mnt/nvme", Available: true, FreeGB: 830.04, TotalGB: 3700, UsedPercent: 78},
		},
		Uptime: 12*24*time.Hour + 4*time.Hour + 30*time.Minute,
	}
}

// DefaultSnapshot は Fixture の 5 ペインのスナップショット。
func DefaultSnapshot() *pane.Snapshot {
	mk := func(id, pos, ag string, st agent.State, top, left int) pane.PaneState {
		return pane.PaneState{
			ID: id, Pos: pos, Session: "0", Top: top, Left: left, Agent: ag, State: st, Title: "agent: " + pos, Cwd: "/mnt/nvme/x",
		}
	}
	return &pane.Snapshot{
		At: At, Host: DefaultHostStatus(),
		Usage:  reportvalues.Usage{Status: reportvalues.StatusPending, Claude: []reportvalues.Account{}, Cursor: []reportvalues.CursorAccount{}, Codex: []reportvalues.Account{}},
		Omp:    reportvalues.Omp{Status: reportvalues.OmpPending},
		Update: pane.UpdateInfo{Status: "pending", Current: "v0.1.4", Apply: pane.ApplyInfo{State: "idle"}},
		Auth: []host.Auth{
			{Agent: "omp", Status: host.AuthUnknown}, {Agent: "claude", Status: host.AuthUnknown}, {Agent: "codex", Status: host.AuthUnknown},
		},
		Layout: pane.Layout{Rows: 2, Cols: []int{2, 3}}, TmuxSession: true,
		Panes: []pane.PaneState{
			mk("%1", "T1", "omp", agent.StateIdle, 0, 0),
			mk("%2", "T2", "omp", agent.StateAsking, 0, 40),
			mk("%3", "B1", "", agent.StateShell, 20, 0),
			mk("%4", "B2", "claude", agent.StateWorking, 20, 40),
			mk("%5", "B3", "", agent.StateUnknown, 20, 80),
		},
	}
}

// UseTwoSessions は config の sessions を 0・1 にし、セッション 0（既定。DefaultSnapshot の 5 ペイン）に、
// セッション 1 の 3 列×2 段の 6 ペイン（%11–%16。全体の位置は 1:T1…、セッション内の位置は T1…）を足す。
func (f *Fixture) UseTwoSessions() {
	f.Cfg.Sessions = []string{"0", "1"}
	locals := []string{"T1", "T2", "T3", "B1", "B2", "B3"}
	agents := []string{"omp", "omp", "claude", "", "omp", "codex"}
	for i, l := range locals {
		f.Registry.Snap.Panes = append(f.Registry.Snap.Panes, pane.PaneState{
			ID: "%" + strconv.Itoa(11+i), Pos: "1:" + l, GlobalPos: "1:" + l, Local: l, Session: "1",
			Top: (i / 3) * 20, Left: (i % 3) * 40, Agent: agents[i], State: agent.StateIdle, Title: "agent: 1:" + l, Cwd: "/mnt/nvme/secret",
		})
	}
}
