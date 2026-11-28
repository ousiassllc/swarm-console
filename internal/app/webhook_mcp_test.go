package app

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/testutil"
)

// bearerTransport は全リクエストに Authorization: Bearer を付ける。
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// connectOps は serve している App の /ops に、発行したトークンで MCP 接続する（待ち受けが始まるまで試す）。
func (g *rig) connectOps(ctx context.Context, token string) *mcp.ClientSession {
	g.t.Helper()
	var cs *mcp.ClientSession
	var lastErr error
	defer func() {
		if g.t.Failed() {
			g.t.Logf("最後の接続エラー: %v", lastErr)
		}
	}()
	testutil.Eventually(g.t, "/ops に MCP 接続できる（待ち受けの開始待ち）", func() bool {
		c := mcp.NewClient(&mcp.Implementation{Name: "webhook-e2e", Version: "1"}, nil)
		cs, lastErr = c.Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint: "http://" + g.app.Cfg.ListenAddr + "/ops", HTTPClient: &http.Client{Transport: bearerTransport{token}},
			DisableStandaloneSSE: true, MaxRetries: -1,
		}, nil)
		if lastErr != nil && ctx.Err() != nil {
			g.t.Fatalf("接続できません: %v (最後の接続エラー: %v)", ctx.Err(), lastErr)
		}
		return lastErr == nil
	})
	g.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// Issue #93: report・host_status・panes_status の出力に、設定した webhook の URL（パス）・署名の鍵・トークンは出ない。
func TestWebhookSecretsNeverAppearInMCPToolOutput(t *testing.T) {
	rcv := newReceiver(t, http.StatusNoContent)
	g := startRig(t, webhookCfg(rcv.srv.URL+"/"+e2ePath))
	var out, errb strings.Builder
	if err := TokenAdd(filepath.Join(g.home, "config.toml"), "ops1", auth.ScopeOps, nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cs := g.connectOps(ctx, strings.TrimSpace(out.String()))

	g.idleRound() // webhook が実際に動いた状態で見る
	for _, name := range []string{"report", "host_status", "panes_status"} {
		for _, args := range []map[string]any{{}, {"format": "json"}} {
			if name != "report" && len(args) > 0 {
				continue
			}
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil || res.IsError {
				t.Fatalf("%s: %v %+v", name, err, res)
			}
			var text strings.Builder
			for _, c := range res.Content {
				if tc, ok := c.(*mcp.TextContent); ok {
					text.WriteString(tc.Text)
				}
			}
			if text.Len() == 0 {
				t.Fatalf("%s の出力が空です", name)
			}
			assertNoSecrets(t, name+" の出力", text.String())
		}
	}
}
