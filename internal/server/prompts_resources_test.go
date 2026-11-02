package server_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

func promptNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range res.Prompts {
		out = append(out, p.Name)
	}
	return out
}

func TestPrompts(t *testing.T) {
	e := newEnv(t)
	if got := promptNames(t, e.connect(t, "/report", "viewer")); !slices.Equal(got, []string{"report"}) {
		t.Errorf("/report の prompts = %v", got)
	}
	ops := e.connect(t, "/ops", "boss")
	if got := promptNames(t, ops); !slices.Equal(sorted(got), []string{"dispatch", "report"}) {
		t.Errorf("/ops の prompts = %v", got)
	}
	// /report に dispatch の prompt は出ない（取得もできない）。
	if _, err := e.connect(t, "/report", "boss").GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "dispatch"}); err == nil {
		t.Error("/report で dispatch の prompt を取得できました")
	}
}

func promptText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]string) string {
	t.Helper()
	res, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("prompt %s: %v", name, err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", res.Messages)
	}
	return res.Messages[0].Content.(*mcp.TextContent).Text
}

func TestPromptTexts(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, "/ops", "boss")
	if got := promptText(t, cs, "report", nil); got != "report ツールを呼び、返った markdown を手を加えずそのまま表示する。" {
		t.Errorf("report prompt = %q", got)
	}
	if got := promptText(t, cs, "report", map[string]string{"lang": "en"}); !strings.Contains(got, "lang=en") || !strings.Contains(got, "手を加えずそのまま表示する") {
		t.Errorf("report prompt (en) = %q", got)
	}
	if _, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "report", Arguments: map[string]string{"lang": "fr"}}); err == nil {
		t.Error("lang=fr を受け付けました")
	}
	got := promptText(t, cs, "dispatch", map[string]string{
		"pane": "T1", "agent": "omp", "task": "やる", "profile": "fix", "extra_rules": "a\nb",
	})
	want := "この引数のまま dispatch ツールを呼び、返った sent と結果を報告する。\n" +
		`引数:` + "\n" + `{"pane":"T1","agent":"omp","task":"やる","profile":"fix","extra_rules":["a","b"]}`
	if got != want {
		t.Errorf("dispatch prompt =\n%s\nwant\n%s", got, want)
	}
	for _, missing := range []map[string]string{{"profile": "fix"}, {"task": "x"}, {"task": "x", "profile": ""}} {
		if _, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "dispatch", Arguments: missing}); err == nil {
			t.Errorf("必須引数なしの dispatch prompt を受け付けました: %v", missing)
		}
	}
}

func TestResources(t *testing.T) {
	e := newEnv(t)
	// ペイン T1 の ID を % で始まる形に（URI では %25 にエスケープする）。
	e.f.Registry.Snap.Panes[0].Pos = "work:T1"
	e.f.Registry.Tails = map[string]pane.Tail{"%1": {Text: "TAIL-MARK"}}
	e.f.Registry.Cards = map[string]*agent.Card{}
	for _, ep := range []struct{ path, token string }{{"/report", "viewer"}, {"/ops", "boss"}} {
		cs := e.connect(t, ep.path, ep.token)
		list, err := cs.ListResources(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var uris []string
		for _, r := range list.Resources {
			uris = append(uris, r.URI)
		}
		for _, want := range []string{"swarm://report", "swarm://pane/work%3AT1", "swarm://notes/work%3AT1", "swarm://pane/B3", "swarm://notes/B3"} {
			if !slices.Contains(uris, want) {
				t.Errorf("%s: resources/list に %s がありません: %v", ep.path, want, uris)
			}
		}
		if len(uris) != 1+2*5 {
			t.Errorf("%s: resources/list の件数 = %d", ep.path, len(uris))
		}
		tpl, err := cs.ListResourceTemplates(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var tpls []string
		for _, r := range tpl.ResourceTemplates {
			tpls = append(tpls, r.URITemplate)
		}
		slices.Sort(tpls)
		if !slices.Equal(tpls, []string{"swarm://notes/{pos}", "swarm://pane/{pos}"}) {
			t.Errorf("%s: templates = %v", ep.path, tpls)
		}
	}
}

func readRes(t *testing.T, cs *mcp.ClientSession, uri string) (*mcp.ResourceContents, error) {
	t.Helper()
	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, err
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %d", len(res.Contents))
	}
	return res.Contents[0], nil
}

func TestReadResources(t *testing.T) {
	e := newEnv(t)
	e.f.Registry.Tails = map[string]pane.Tail{"%1": {Text: "TAIL-MARK"}}
	cs := e.connect(t, "/report", "viewer")

	rep, err := readRes(t, cs, "swarm://report")
	if err != nil || rep.MIMEType != "text/markdown" {
		t.Fatalf("swarm://report: %v %+v", err, rep)
	}
	want, isErr := callText(t, cs, "report", nil)
	if isErr || rep.Text != want {
		t.Errorf("swarm://report が report ツールと違います")
	}
	// 位置名でも、% を %25 にした tmux ID でも読める。
	for _, uri := range []string{"swarm://pane/T1", "swarm://pane/t1", "swarm://pane/%251"} {
		c, err := readRes(t, cs, uri)
		if err != nil || c.MIMEType != "text/markdown" || !strings.Contains(c.Text, "TAIL-MARK") {
			t.Errorf("%s: %v %+v", uri, err, c)
		}
	}
	for _, uri := range []string{"swarm://notes/T1", "swarm://notes/%251"} {
		c, err := readRes(t, cs, uri)
		if err != nil || c.MIMEType != "application/json" || c.Text != `{"pane":"T1","id":"%1","note":null}` {
			t.Errorf("%s: %v %+v", uri, err, c)
		}
	}
}

// 解決できない位置は JSON-RPC の -32002 ＋ pane_not_found の本文。
func TestReadResourceNotFound(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, "/ops", "boss")
	for _, uri := range []string{"swarm://pane/Z9", "swarm://notes/Z9", "swarm://pane/%25999", "swarm://pane/", "swarm://notes/%zz"} {
		_, err := readRes(t, cs, uri)
		var je *jsonrpc.Error
		if !errors.As(err, &je) || je.Code != -32002 {
			t.Errorf("%s: err = %v, want -32002", uri, err)
			continue
		}
		if !strings.Contains(string(je.Data), `"code":"pane_not_found"`) {
			t.Errorf("%s: data = %s", uri, je.Data)
		}
	}
}
