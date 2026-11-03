package view

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

type sessionArgs struct {
	Session *string `json:"session"`
}

func (e *env) panesStatusTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "panes_status",
		Description: descPanesStatus,
		Scope:       toolkit.ScopeReport,
		Schema:      toolkit.Object(nil, toolkit.Props{"session": toolkit.SessionProp()}),
		Handler:     e.panesStatus,
	}
}

func (e *env) panesStatus(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a sessionArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	snap, err := e.Scoped(ctx, c, a.Session)
	if err != nil {
		return toolkit.Result{}, err
	}
	b, err := render.PanesStatusJSON(snap)
	if err != nil {
		return toolkit.Result{}, apperr.Wrap(apperr.Internal, err)
	}
	return toolkit.Result{Text: string(b)}, nil
}

type agentCounts struct {
	OMP    int `json:"omp"`
	Claude int `json:"claude"`
	Codex  int `json:"codex"`
}

// hostStatusJSON は API 仕様 §5.3。cpu・swap・disk・usage・omp・update は report の JSON と同じ形・同じ値（FR-REP-17）。
type hostStatusJSON struct {
	Load        [3]float64        `json:"load"`
	CPUs        int               `json:"cpus"`
	MemTotalGB  float64           `json:"mem_total_gb"`
	MemFreeGB   float64           `json:"mem_free_gb"`
	CPU         render.CPUJSON    `json:"cpu"`
	Swap        render.SwapJSON   `json:"swap"`
	Disk        []render.DiskJSON `json:"disk"`
	TmuxSession bool              `json:"tmux_session"`
	Agents      agentCounts       `json:"agents"`
	Uptime      string            `json:"uptime"`
	Usage       render.UsageJSON  `json:"usage"`
	Omp         render.OmpJSON    `json:"omp"`
	Update      render.UpdateJSON `json:"update"`
}

func (e *env) hostStatusTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "host_status",
		Description: descHostStatus,
		Scope:       toolkit.ScopeReport,
		Schema:      toolkit.Object(nil, toolkit.Props{"session": toolkit.SessionProp()}),
		Handler:     e.hostStatus,
	}
}

func (e *env) hostStatus(ctx context.Context, c auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a sessionArgs
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	// 負荷・メモリ・使用状況などはホスト全体のまま。session で絞るのは agents と tmux_session だけ。
	snap, err := e.Scoped(ctx, c, a.Session)
	if err != nil {
		return toolkit.Result{}, err
	}
	h := snap.Host
	out := hostStatusJSON{
		CPUs: h.CPUs, MemTotalGB: round1(h.MemTotalGB), MemFreeGB: round1(h.MemFreeGB),
		CPU: render.NewCPUJSON(snap), Swap: render.NewSwapJSON(snap), Disk: render.NewDiskJSON(snap),
		TmuxSession: snap.TmuxSession, Agents: countAgents(snap.Panes), Uptime: formatUptime(h.Uptime),
		Usage: render.NewUsageJSON(snap), Omp: render.NewOmpJSON(snap), Update: render.NewUpdateJSON(snap),
	}
	for i, l := range h.Load {
		out.Load[i] = round1(l)
	}
	return toolkit.MarshalResult(out)
}

func countAgents(panes []pane.PaneState) agentCounts {
	var c agentCounts
	for i := range panes {
		switch panes[i].Agent {
		case "omp":
			c.OMP++
		case "claude":
			c.Claude++
		case "codex":
			c.Codex++
		}
	}
	return c
}

// formatUptime は稼働時間を `12d 4h` 形式にする。1 日未満は `4h 5m`、1 時間未満は `5m`。
func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days, hours, mins := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

type authEntryJSON struct {
	Agent     string  `json:"agent"`
	Status    string  `json:"status"`
	ExpiresAt *string `json:"expires_at"`
}

func (e *env) agentAuthStatusTool() toolkit.Tool {
	return toolkit.Tool{
		Name:        "agent_auth_status",
		Description: descAgentAuthStatus,
		Scope:       toolkit.ScopeReport,
		Schema:      toolkit.Object(nil, toolkit.Props{}),
		Handler:     e.agentAuthStatus,
	}
}

func (e *env) agentAuthStatus(_ context.Context, _ auth.Client, raw json.RawMessage) (toolkit.Result, error) {
	var a struct{}
	if err := toolkit.DecodeArgs(raw, &a); err != nil {
		return toolkit.Result{}, err
	}
	now := e.Now()
	names := agent.Builtin()
	entries := make([]authEntryJSON, 0, len(names))
	for _, name := range names {
		var authFile string
		if ov, ok := e.Cfg.Agents[name]; ok && ov.AuthFile != nil {
			authFile = *ov.AuthFile
		}
		st := e.Host.AuthStatus(name, authFile, now)
		ent := authEntryJSON{Agent: name, Status: st.Status}
		if st.ExpiresAt != nil {
			s := st.ExpiresAt.Local().Format(time.RFC3339)
			ent.ExpiresAt = &s
		}
		entries = append(entries, ent)
	}
	return toolkit.MarshalResult(struct {
		Agents []authEntryJSON `json:"agents"`
	}{entries})
}
