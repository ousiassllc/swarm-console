package integration

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

func TestHostStatus(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	omp := h.NewAgent("omp", "idle")
	h.NewAgent("codex", "idle")
	h.WaitState(omp, "idle")
	var got struct {
		Load        []float64 `json:"load"`
		CPUs        int       `json:"cpus"`
		MemTotalGB  float64   `json:"mem_total_gb"`
		Disk        []struct{ Mount string }
		TmuxSession bool           `json:"tmux_session"`
		Agents      map[string]int `json:"agents"`
		Uptime      string         `json:"uptime"`
	}
	h.Report().Must("host_status", nil).Decode(t, &got)
	if len(got.Load) != 3 || got.CPUs < 1 || got.MemTotalGB <= 0 || !got.TmuxSession || got.Uptime == "" ||
		len(got.Disk) != 1 || got.Disk[0].Mount != "/" {
		t.Errorf("host_status = %+v", got)
	}
	h.Eventually("agents の数が反映される", func() bool {
		h.Report().Must("host_status", nil).Decode(t, &got)
		return got.Agents["omp"] == 1 && got.Agents["codex"] == 1 && got.Agents["claude"] == 0
	})
	if code := h.Report().Call("host_status", map[string]any{"x": 1}).Code(t); code != "invalid_args" {
		t.Errorf("未知の引数: %s", code)
	}
}

// agent_auth_status: 認証ファイルは期限だけを読む。値（ダミー）は応答に出ない。
func TestAgentAuthStatus(t *testing.T) {
	h := harness.Start(t, harness.Options{Agents: map[string]map[string]string{"omp": {"auth_file": `"~/omp-auth.json"`}}})
	soon := time.Now().Add(24 * time.Hour)
	b64 := base64.RawURLEncoding.EncodeToString
	jwt := fmt.Sprintf("%s.%s.%s", b64([]byte(`{"alg":"none"}`)), b64([]byte(fmt.Sprintf(`{"exp":%d}`, soon.Unix()))), "dummy-signature")
	const claudeSecret, ompSecret = "dummy-claude-access-token", "dummy-omp-token" //nolint:gosec // 応答に出ないことを確かめるダミーの値
	claudeExp := time.Now().Add(30 * 24 * time.Hour)
	h.WriteFile(filepath.Join(h.Dir, ".claude", ".credentials.json"),
		fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d}}`, claudeSecret, claudeExp.UnixMilli()))
	h.WriteFile(filepath.Join(h.Dir, ".codex", "auth.json"), fmt.Sprintf(`{"tokens":{"access_token":%q}}`, jwt))
	h.WriteFile(filepath.Join(h.Dir, "omp-auth.json"),
		fmt.Sprintf(`{"token":%q,"expires_at":%q}`, ompSecret, time.Now().Add(-time.Hour).Format(time.RFC3339)))

	res := h.Report().Must("agent_auth_status", nil)
	var got struct {
		Agents []struct {
			Agent     string  `json:"agent"`
			Status    string  `json:"status"`
			ExpiresAt *string `json:"expires_at"`
		} `json:"agents"`
	}
	res.Decode(t, &got)
	want := map[string]string{"omp": "expired", "claude": "valid", "codex": "expiring_soon"}
	if len(got.Agents) != 3 {
		t.Fatalf("agents = %+v", got.Agents)
	}
	for i, name := range []string{"omp", "claude", "codex"} {
		a := got.Agents[i]
		if a.Agent != name || a.Status != want[name] || a.ExpiresAt == nil {
			t.Errorf("%s: %+v, want %s", name, a, want[name])
			continue
		}
		if ts, err := time.Parse(time.RFC3339, *a.ExpiresAt); err != nil || ts.IsZero() {
			t.Errorf("%s の expires_at: %v", name, err)
		}
	}
	for _, secret := range []string{claudeSecret, ompSecret, jwt, "dummy-signature"} {
		if strings.Contains(res.Text, secret) {
			t.Errorf("応答に認証ファイルの中身が出ている: %s", secret)
		}
	}
}
