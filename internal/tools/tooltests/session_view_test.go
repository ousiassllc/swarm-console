package tooltests_test

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

func sessionsOf(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, s := range m["sessions"].([]any) {
		out = append(out, s.(map[string]any))
	}
	return out
}

// 受け入れ 6: sessions_list は許可されたセッションの layout・panes を返す。all=true でも許可外は id だけのスタブ。
func TestSessionsList(t *testing.T) {
	f := twoSessions(t)

	got := sessionsOf(t, mustAs(t, f, kana, "sessions_list", `{}`))
	if len(got) != 1 || got[0]["id"] != "0" || got[0]["allowed"] != true || got[0]["operable"] != true {
		t.Fatalf("kana の既定 = %v（許可されたセッションだけ）", got)
	}
	if want := []any{"T1", "T2", "B1", "B2", "B3"}; !reflect.DeepEqual(got[0]["panes"], want) {
		t.Errorf("panes = %v", got[0]["panes"])
	}
	if want := map[string]any{"rows": float64(2), "cols": []any{float64(2), float64(3)}}; !reflect.DeepEqual(got[0]["layout"], want) {
		t.Errorf("layout = %v", got[0]["layout"])
	}

	all := sessionsOf(t, mustAs(t, f, kana, "sessions_list", `{"all":true}`))
	if len(all) != 2 || all[0]["id"] != "0" || all[1]["id"] != "1" {
		t.Fatalf("all=true = %v（config の順）", all)
	}
	// スタブは id・allowed・operable だけ（layout・panes は出さない）。
	if want := map[string]any{"id": "1", "allowed": false, "operable": false}; !reflect.DeepEqual(all[1], want) {
		t.Errorf("スタブ = %v, want %v", all[1], want)
	}
	if text, _ := callAs(t, f, kana, toolkit.EndpointOps, "sessions_list", `{"all":true}`); strings.Contains(text, "secret") || strings.Contains(text, "%11") {
		t.Errorf("許可外のペインの中身が出ています: %s", text)
	}

	both := sessionsOf(t, mustAs(t, f, boss, "sessions_list", `{}`))
	if len(both) != 2 || both[1]["id"] != "1" || both[1]["allowed"] != true {
		t.Fatalf("boss = %v", both)
	}
	if want := []any{"T1", "T2", "T3", "B1", "B2", "B3"}; !reflect.DeepEqual(both[1]["panes"], want) {
		t.Errorf("セッション 1 の panes = %v", both[1]["panes"])
	}
	if e := errAs(t, f, kana, "sessions_list", `{"all":"yes"}`); e.Code != apperr.InvalidArgs || e.Details["field"] != "all" {
		t.Errorf("all の型違い: %s %v", e.Code, e.Details)
	}
}

// operable は「許可あり」かつ「ops スコープ」かつ「/ops 経由」のときだけ true。
func TestSessionsListOperable(t *testing.T) {
	f := twoSessions(t)
	for _, c := range []struct {
		name     string
		client   auth.Client
		endpoint string
		want     bool
	}{
		{"ops トークン・/ops", natsumi, toolkit.EndpointOps, true},
		{"ops トークン・/report", natsumi, toolkit.EndpointReport, false},
		{"report トークン・/report", viewer, toolkit.EndpointReport, false},
	} {
		text, err := callAs(t, f, c.client, c.endpoint, "sessions_list", `{}`)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		s := sessionsOf(t, m)
		if len(s) != 1 || s[0]["id"] != "1" || s[0]["allowed"] != true || s[0]["operable"] != c.want {
			t.Errorf("%s: %v", c.name, s)
		}
	}
	// 窓口が分からない呼び出し（ctx に載っていない）は、操作できるとは言わない。
	tool, _ := f.Tools.Lookup(toolkit.EndpointOps, "sessions_list")
	res, err := tool.Handler(context.Background(), natsumi, json.RawMessage(`{}`))
	if err != nil || !strings.Contains(res.Text, `"operable":false`) {
		t.Errorf("窓口なし: %s %v", res.Text, err)
	}
}

// config にあるが tmux に無いセッションは、許可されていれば空の layout・panes。
func TestSessionsListMissingSession(t *testing.T) {
	f := twoSessions(t)
	f.Cfg.Sessions = []string{"0", "1", "2"}
	c := auth.Client{Name: "c", Scope: auth.ScopeOps, AllowedSessions: []string{"2"}}
	got := sessionsOf(t, mustAs(t, f, c, "sessions_list", `{}`))
	if len(got) != 1 || got[0]["id"] != "2" {
		t.Fatalf("%v", got)
	}
	if want := (map[string]any{"rows": float64(0), "cols": []any{}}); !reflect.DeepEqual(got[0]["layout"], want) || !reflect.DeepEqual(got[0]["panes"], []any{}) {
		t.Errorf("layout=%v panes=%v", got[0]["layout"], got[0]["panes"])
	}
}

// report は panes・layout・attention のペインをセッションで絞り、ログイン期限などホスト全体のものは絞らない。
func TestReportSessionScope(t *testing.T) {
	f := twoSessions(t)
	f.Registry.Snap.Auth[1] = hostAuthExpired("claude")
	f.Registry.Snap.Panes[1].State = agent.StateAsking // セッション 0 の T2
	f.Registry.Snap.Panes[6].State = agent.StateAsking // セッション 1 の T2（%12）

	for _, c := range []struct {
		client    auth.Client
		wantPanes []string
		attention string
	}{
		{kana, []string{"T1", "T2", "B1", "B2", "B3"}, "T2"},
		{natsumi, []string{"T1", "T2", "T3", "B1", "B2", "B3"}, "T2"},
	} {
		m := mustAs(t, f, c.client, "report", `{"format":"json"}`)
		if got := posOf(t, m); !reflect.DeepEqual(got, c.wantPanes) {
			t.Errorf("%s の panes = %v", c.client.Name, got)
		}
		var asking, authItems int
		for _, a := range m["attention"].([]any) {
			item := a.(map[string]any)
			switch item["kind"] {
			case "asking":
				asking++
			case "auth_expired":
				authItems++
			}
		}
		if asking != 1 || authItems != 1 {
			t.Errorf("%s の attention = %v（質問待ちは自分のセッションの 1 つだけ、ログイン期限は全体）", c.client.Name, m["attention"])
		}
		if m["auth"] == nil || m["usage"] == nil || m["host"] == nil {
			t.Errorf("全体の項目が消えています: %v", m)
		}
	}
	md, err := callAs(t, f, kana, toolkit.EndpointReport, "report", `{}`)
	if err != nil || strings.Contains(md, "1:T") {
		t.Errorf("kana の markdown に他セッションの位置が出ています: %v\n%s", err, md)
	}
	// pane と session の組。許可外は session_forbidden、session と食い違う pane は invalid_args。
	if e := errAs(t, f, kana, "report", `{"pane":"%12"}`); e.Code != apperr.SessionForbidden {
		t.Errorf("許可外の pane: %s", e.Code)
	}
	if text, err := callAs(t, f, natsumi, toolkit.EndpointReport, "report", `{"pane":"T2"}`); err != nil || !strings.Contains(text, "%12") {
		t.Errorf("natsumi の T2 = %v %s", err, text)
	}
}

// 最終報告のファイル名は全体の位置から決まるので、別セッションの同じ T1 が同じファイルにならない。
func TestDispatchReportPathPerSession(t *testing.T) {
	f := twoSessions(t)
	for _, c := range []struct {
		client auth.Client
		want   string
	}{{kana, "/T1.md"}, {natsumi, "/1_T1.md"}} {
		f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Sent: true}
		f.Registry.Snap.Panes[0].State, f.Registry.Snap.Panes[5].State = agent.StateIdle, agent.StateIdle
		m := mustAs(t, f, c.client, "dispatch", `{"pane":"T1","agent":"omp","task":"t","profile":"fix"}`)
		if sent, _ := m["sent"].(string); !strings.Contains(sent, c.want) {
			t.Errorf("%s の指示文に %s が無い: %s", c.client.Name, c.want, sent)
		}
	}
	got := f.Ops.LastDispatch.Text
	if strings.Contains(got, "/T1.md") && !strings.Contains(got, "/1_T1.md") {
		t.Errorf("セッション 1 の指示文が既定セッションのファイルを指しています: %s", got)
	}
}

// 後方互換: sessions=["0"] だけ・許可を書かないトークンなら、session 引数なしで全ツールが従来どおり動く（受け入れ 7）。
func TestSingleSessionKeepsWorkingWithoutSessionArg(t *testing.T) {
	f := toolstest.New(t)
	if got := posOf(t, mustAs(t, f, boss, "panes_status", `{}`)); !reflect.DeepEqual(got, []string{"T1", "T2", "B1", "B2", "B3"}) {
		t.Errorf("panes_status = %v", got)
	}
	if m := mustAs(t, f, boss, "pane_tail", `{"pane":"T1"}`); m["pane"] != "T1" {
		t.Errorf("pane_tail = %v", m)
	}
	if m := mustAs(t, f, boss, "pane_tail", `{"pane":"%2"}`); m["pane"] != "T2" {
		t.Errorf("ID 指定 = %v", m)
	}
	for _, c := range []struct{ tool, args string }{
		{"ask_card", `{"pane":"T2"}`}, {"report_read", `{"pane":"T1"}`}, {"note_get", `{"pane":"T1"}`}, {"host_status", `{}`},
		{"report", `{"format":"json"}`}, {"report", `{"pane":"T1"}`}, {"sessions_list", `{}`},
		{"note_set", `{"pane":"T1","set":{"from":"x"}}`}, {"note_log", `{"pane":"T1","text":"x"}`},
		{"send_text", `{"pane":"B1","text":"x"}`}, {"send_escape", `{"pane":"B2","confirm":true}`},
		{"pane_close", `{"pane":"B1"}`}, {"ask_pick", `{"pane":"T2","n":1}`},
	} {
		if _, err := callAs(t, f, boss, toolkit.EndpointOps, c.tool, c.args); err != nil {
			t.Errorf("%s(%s): %v", c.tool, c.args, err)
		}
	}
	// 明示した session は、1 つだけの許可でも使える。
	mustAs(t, f, boss, "panes_status", `{"session":"0"}`)
	if e := errAs(t, f, boss, "panes_status", `{"session":"1"}`); e.Code != apperr.InvalidArgs {
		t.Errorf("config 外の session: %s", e.Code)
	}
	// config の外の許可しか持たない（空の共通部分）トークンは何も見えない。
	got := sessionsOf(t, mustAs(t, f, auth.Client{Name: "x", Scope: auth.ScopeOps, AllowedSessions: []string{"1"}}, "sessions_list", `{}`))
	if len(got) != 0 {
		t.Errorf("sessions_list = %v", got)
	}
}

func TestSessionPropOnPaneTools(t *testing.T) {
	f := toolstest.New(t)
	var withSession, without []string
	for _, tl := range f.Tools.ForEndpoint(toolkit.EndpointOps) {
		props := tl.Schema.(map[string]any)["properties"].(map[string]any)
		if _, ok := props["session"]; ok {
			withSession = append(withSession, tl.Name)
		} else {
			without = append(without, tl.Name)
		}
	}
	slices.Sort(without)
	if want := []string{"agent_auth_status", "dir_list", "file_get", "note_history", "sessions_list"}; !slices.Equal(without, want) {
		t.Errorf("session 引数が無いツール = %v, want %v", without, want)
	}
	if len(withSession) != 15 {
		t.Errorf("session 引数を持つツール = %d (%v)", len(withSession), withSession)
	}
}
