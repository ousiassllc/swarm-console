package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// valuesRig は serve の収集器が呼ぶ omp の代わり。呼ばれたコマンドを記録し、用意した出力を返す。
type valuesRig struct {
	clk *clock.Fake

	mu    sync.Mutex
	calls []reportvalues.Command
	out   map[reportvalues.Command][]byte
}

func newValuesRig() *valuesRig {
	return &valuesRig{clk: clock.NewFake(clkStart), out: map[reportvalues.Command][]byte{}}
}

func (v *valuesRig) Run(_ context.Context, c reportvalues.Command) ([]byte, string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls = append(v.calls, c)
	out, ok := v.out[c]
	if !ok {
		return nil, reportvalues.ReasonNotFound
	}
	return out, ""
}

func (v *valuesRig) called() []reportvalues.Command {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]reportvalues.Command(nil), v.calls...)
}

func (g *serveRig) callReport(args map[string]any) string {
	g.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "values", Version: "1"}, nil)
	cs, err := c.Connect(g.ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://" + g.addr + "/report", HTTPClient: &http.Client{Transport: bearerTransport{g.token}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		g.t.Fatalf("接続: %v", err)
	}
	defer func() { _ = cs.Close() }()
	res, err := cs.CallTool(g.ctx, &mcp.CallToolParams{Name: "report", Arguments: args})
	if err != nil || res.IsError {
		g.t.Fatalf("report: %v %+v", err, res)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

// reportUntil は snapshot_ttl 後の report が cond を満たすまで、上限つきで繰り返す。
func (g *serveRig) reportJSONUntil(what string, cond func(map[string]any) bool) map[string]any {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var out map[string]any
		if err := json.Unmarshal([]byte(g.callReport(map[string]any{"format": "json", "lang": "en"})), &out); err != nil {
			g.t.Fatal(err)
		}
		if cond(out) {
			return out
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("%s にならない: %v", what, out)
		}
		time.Sleep(20 * time.Millisecond) // 上限つきのポーリング間隔（スナップショットの更新待ち）
	}
}

// serve の配線：収集器が引数固定の 2 コマンドだけを決まった間隔で実行し、結果が report（json・markdown）に入る。
// 起動直後は pending、usage_check = false なら disabled で実行しない。メールは出ない。
func TestServeReportValuesFromCollector(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "update_auto = false\n")
	usage, err := os.ReadFile("../../internal/reportvalues/testdata/usage_ok.json")
	if err != nil {
		t.Fatal(err)
	}
	update, err := os.ReadFile("../../internal/reportvalues/testdata/omp_update_available.txt")
	if err != nil {
		t.Fatal(err)
	}
	g.values.out[reportvalues.CmdUsage] = usage
	g.values.out[reportvalues.CmdUpdateCheck] = update
	g.release("v0.1.4", newBin)
	g.start()

	pending := g.reportJSONUntil("pending", func(m map[string]any) bool { return m["usage"] != nil })
	if pending["usage"].(map[string]any)["status"] != "pending" || pending["omp"].(map[string]any)["status"] != "pending" {
		t.Fatalf("起動直後は pending: %v", pending["usage"])
	}
	if n := len(g.values.called()); n != 0 {
		t.Fatalf("最初の待ちの前に omp を実行した（%d）", n)
	}
	for g.values.clk.Waiters() < 2 {
		time.Sleep(5 * time.Millisecond) // 収集器が待ちに入るまで（上限は go test の timeout）
	}
	g.values.clk.Advance(reportvalues.OmpFirstDelay)
	got := g.reportJSONUntil("usage・omp が入る", func(m map[string]any) bool {
		return m["usage"].(map[string]any)["status"] == "ok" && m["omp"].(map[string]any)["status"] == "update_available"
	})
	if cl := got["usage"].(map[string]any)["claude"].([]any); len(cl) != 5 {
		t.Errorf("Claude は全アカウント（5）: %d", len(cl))
	}
	if o := got["omp"].(map[string]any); o["current"] != "18.14.2" || o["latest"] != "18.15.0" {
		t.Errorf("omp = %v", o)
	}
	md := g.callReport(map[string]any{"lang": "ja"})
	for _, want := range []string{"## 使用状況", "### Claude", "omp 18.14.2（新しい版 18.15.0 あり）"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown に %q が無い", want)
		}
	}
	for _, bad := range []string{"@", ".example", "accountId"} {
		if strings.Contains(md, bad) {
			t.Errorf("markdown に %q が出ている", bad)
		}
	}
	// 実行したのは引数固定の 2 コマンドだけ（NFR-SEC-7）。
	for _, c := range g.values.called() {
		if c != reportvalues.CmdUsage && c != reportvalues.CmdUpdateCheck {
			t.Errorf("知らないコマンド: %v", c)
		}
	}
	g.cancel()
	g.wait()
}

func TestServeUsageCheckFalse(t *testing.T) {
	g := newServeRig(t, "v0.1.4", "usage_check = false\nupdate_check = false\n")
	g.start()
	got := g.reportJSONUntil("disabled", func(m map[string]any) bool { return m["usage"] != nil })
	if got["usage"].(map[string]any)["status"] != "disabled" || got["omp"].(map[string]any)["status"] != "disabled" {
		t.Fatalf("止めたら disabled: %v %v", got["usage"], got["omp"])
	}
	if w := g.values.clk.Waiters(); w != 0 {
		t.Errorf("収集器の待ちがある（%d）", w)
	}
	g.values.clk.Advance(48 * time.Hour)
	if n := len(g.values.called()); n != 0 {
		t.Errorf("止めたのに omp を実行した（%d）", n)
	}
	g.cancel()
	g.wait()
}
