package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
	"github.com/ousiassllc/swarm-console/test/fakeagent/fakeagenttest"
)

// paneStates は panes_status（ops）の、ペイン ID → state。serve が実際に見ているスナップショットの内容。
func (g *serveRig) paneStates() map[string]string {
	g.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "panes", Version: "1"}, nil)
	cs, err := c.Connect(g.ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://" + g.addr + "/ops", HTTPClient: &http.Client{Transport: bearerTransport{g.token}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		g.t.Fatalf("接続: %v", err)
	}
	defer func() { _ = cs.Close() }()
	res, err := cs.CallTool(g.ctx, &mcp.CallToolParams{Name: "panes_status", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		g.t.Fatalf("panes_status: %v %+v", err, res)
	}
	var out struct {
		Panes []struct{ ID, State string } `json:"panes"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		g.t.Fatalf("panes_status の解釈: %v", err)
	}
	states := map[string]string{}
	for _, p := range out.Panes {
		states[p.ID] = p.State
	}
	return states
}

// untilPaneState は panes_status が id を state と答えるまで、上限つきで繰り返す。
func (g *serveRig) untilPaneState(id, state string) {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := g.paneStates()
		if got[id] == state {
			return
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("ペイン %s が %s になりません: %v", id, state, got)
		}
		time.Sleep(20 * time.Millisecond) // 上限つきのポーリング間隔（スナップショットの更新待ち）
	}
}

// working のペインがあっても、自己更新は待たず・見送らずに入れ替えて終了コード 0 で終わる（FR-UPD-16・P-54・NFR-TEST-16）。
// 実 tmux の専用ソケットに擬似エージェント（omp working）のペインを立て、serve のスナップショットが working と見ていることを
// 確かめてから clock を進める。ペインを見て待つ・見送る実装（mam の SafeGate 相当）に変わると、入れ替わらずここで落ちる。
func TestServeSelfUpdateProceedsWithWorkingPane(t *testing.T) {
	bin := fakeagenttest.Install(t, t.TempDir()) // HOME を差し替える前に go build する（ビルドキャッシュの置き場を変えない）
	tm := tmuxtest.New(t)
	g := newServeRigOnSocket(t, "v0.1.4", "", tm.Socket)
	pane := tm.NewSession("0", 120, 40, fmt.Sprintf("PATH=%s:$PATH exec omp working", bin))
	tm.WaitForText(pane, "READY")
	g.release("v0.1.5", newBin)
	g.start()

	g.untilPaneState(pane, "working")
	g.untilWaiting(1)
	g.clk.Advance(time.Minute)

	if code := g.wait(); code != 0 {
		t.Fatalf("終了コード = %d\n%s", code, g.stderr.String())
	}
	if g.read("swarm-console") != newBin || g.read("swarm-console.old") != oldBin {
		t.Errorf("入れ替わっていない: bin=%q old=%q", g.read("swarm-console"), g.read("swarm-console.old"))
	}
	if got := g.files(); !slices.Equal(got, []string{"swarm-console", "swarm-console.old", "swarm-console.update-pending"}) {
		t.Errorf("置き場のファイル = %v", got)
	}
	// ペインとエージェントは tmux に残る（FR-UPD-16）。
	if out := tm.Run("list-panes", "-a", "-F", "#{pane_id}"); !slices.Contains(strings.Fields(out), pane) {
		t.Errorf("ペイン %s が残っていません: %q", pane, out)
	}
}
