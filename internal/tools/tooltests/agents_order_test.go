package tooltests_test

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// authRecorder は AuthStatus に渡されたエージェント名を呼ばれた順に残す host.Host。
type authRecorder struct {
	*toolstest.Host
	mu    sync.Mutex
	asked []string
}

func (r *authRecorder) AuthStatus(agent, authFile string, now time.Time) host.Auth {
	r.mu.Lock()
	r.asked = append(r.asked, agent)
	r.mu.Unlock()
	return r.Host.AuthStatus(agent, authFile, now)
}

func (r *authRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.asked
	r.asked = nil
	return out
}

type authAgentsJSON struct {
	Agents []struct {
		Agent  string `json:"agent"`
		Status string `json:"status"`
	} `json:"agents"`
}

func (a authAgentsJSON) pairs() []string {
	var out []string
	for _, e := range a.Agents {
		out = append(out, e.Agent+":"+e.Status)
	}
	return out
}

// report の auth と agent_auth_status は、同じ pane.Agents（1 か所）の順・対象でログイン期限を読む（FR-REP-16）。
// report は本物の SnapshotRegistry が作るスナップショットから、agent_auth_status は host を直接読んで出すので、
// どちらかが自前の一覧を持つと、host へ聞く名前・並びか出力の並びがずれる。
func TestReportAuthAndAgentAuthStatusShareAgents(t *testing.T) {
	f := toolstest.New(t)
	exp := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	rec := &authRecorder{Host: &toolstest.Host{
		St: toolstest.DefaultHostStatus(),
		Auth: map[string]host.Auth{
			"omp":    {Agent: "omp", Status: host.AuthExpired},
			"claude": {Agent: "claude", Status: host.AuthValid, ExpiresAt: &exp},
			"codex":  {Agent: "codex", Status: host.AuthExpiringSoon, ExpiresAt: &exp},
		},
	}}
	env := panetest.NewEnv()
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: f.Cfg, Tmux: env.Tmux, Host: rec, Adapters: env.Adapters, Notes: env.Notes, Clock: f.Clock, FS: env.FS,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.Deps.Registry, f.Deps.Host = reg, rec
	if f.Tools, err = tools.New(f.Deps); err != nil {
		t.Fatal(err)
	}

	reportText, err := call(t, f, "report", `{"format":"json"}`)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Auth authAgentsJSON `json:"auth"`
	}
	if err := json.Unmarshal([]byte(reportText), &report); err != nil {
		t.Fatalf("report が JSON ではありません: %v", err)
	}
	askedByReport := rec.take()

	statusText, err := call(t, f, "agent_auth_status", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var status authAgentsJSON
	if err := json.Unmarshal([]byte(statusText), &status); err != nil {
		t.Fatalf("agent_auth_status が JSON ではありません: %v", err)
	}
	askedByStatus := rec.take()

	if !slices.Equal(askedByReport, pane.Agents) || !slices.Equal(askedByStatus, pane.Agents) {
		t.Errorf("host へ聞いた順: report=%v agent_auth_status=%v, want %v", askedByReport, askedByStatus, pane.Agents)
	}
	if !slices.Equal(report.Auth.pairs(), status.pairs()) {
		t.Errorf("出力が違います: report.auth=%v agent_auth_status=%v", report.Auth.pairs(), status.pairs())
	}
	want := []string{"omp:expired", "claude:valid", "codex:expiring_soon"}
	if !slices.Equal(status.pairs(), want) {
		t.Errorf("agent_auth_status = %v, want %v", status.pairs(), want)
	}
}
