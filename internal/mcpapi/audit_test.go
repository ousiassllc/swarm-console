package mcpapi_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/mcpapi"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

type recorder struct {
	mu     sync.Mutex
	events []mcpapi.Event
}

func (r *recorder) add(_ context.Context, e mcpapi.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) all() []mcpapi.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mcpapi.Event(nil), r.events...)
}

func connect(t *testing.T, f *toolstest.Fixture, rec *recorder) *mcp.ClientSession {
	t.Helper()
	srv := mcpapi.NewOpsServer(mcpapi.Options{Tools: f.Tools, Audit: rec.add})
	ts := httptest.NewServer(mcpapi.NewHTTPHandler(srv))
	t.Cleanup(ts.Close)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: ts.Client(), DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// 位置・固定名が 40 字を超えても resource から読める（監査ログ用の切り詰めを解決に使わない）。
func TestResourceReadsLongPositionName(t *testing.T) {
	f := toolstest.New(t)
	long := strings.Repeat("n", 41) + "x"
	f.Registry.Snap.Panes[0].Pos = long
	rec := &recorder{}
	cs := connect(t, f, rec)
	for _, uri := range []string{"swarm://pane/" + long, "swarm://notes/" + long} {
		res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil || len(res.Contents) != 1 || res.Contents[0].Text == "" {
			t.Errorf("%s: %v %+v", uri, err, res)
		}
	}
	for _, e := range rec.all() {
		if len([]rune(e.Pane)) > 40 {
			t.Errorf("監査ログの pane が 40 字を超えています: %q", e.Pane)
		}
	}
}

// 登録されていないツール名・prompt 名は、クライアントが付けた文字列を監査ログに残さず unknown にする。
func TestAuditTargetIsRegisteredNamesOnly(t *testing.T) {
	f := toolstest.New(t)
	rec := &recorder{}
	cs := connect(t, f, rec)
	bad := strings.Repeat("TOOL-MARKER-", 5000)
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: bad}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: bad}); err == nil {
		t.Fatal("未知の prompt が取得できました")
	}
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "host_status", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range rec.all() {
		if strings.Contains(e.Target, "MARKER") {
			t.Fatalf("未知の名前が監査ログに入っています（%d 字）", len(e.Target))
		}
		if e.Method == "tools/call" || e.Method == "prompts/get" {
			got = append(got, e.Method+":"+e.Target)
		}
	}
	want := []string{"tools/call:unknown", "prompts/get:unknown", "tools/call:host_status"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("監査対象 = %v, want %v", got, want)
	}
}
