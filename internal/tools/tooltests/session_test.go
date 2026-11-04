package tooltests_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

var (
	kana    = auth.Client{Name: "kana", Scope: auth.ScopeOps, AllowedSessions: []string{"0"}}
	natsumi = auth.Client{Name: "natsumi", Scope: auth.ScopeOps, AllowedSessions: []string{"1"}}
	boss    = auth.Client{Name: "boss", Scope: auth.ScopeOps} // 許可を書かない＝config の sessions すべて
	viewer  = auth.Client{Name: "viewer", Scope: auth.ScopeReport, AllowedSessions: []string{"1"}}
)

// callAs は endpoint に出ているツール name を、クライアント c として呼ぶ。
func callAs(t *testing.T, f *toolstest.Fixture, c auth.Client, endpoint, name, args string) (string, error) {
	t.Helper()
	tool, ok := f.Tools.Lookup(endpoint, name)
	if !ok {
		t.Fatalf("%s にツール %s がありません", endpoint, name)
	}
	res, err := tool.Handler(toolkit.WithEndpoint(context.Background(), endpoint), c, json.RawMessage(args))
	return res.Text, err
}

func mustAs(t *testing.T, f *toolstest.Fixture, c auth.Client, name, args string) map[string]any {
	t.Helper()
	text, err := callAs(t, f, c, toolkit.EndpointOps, name, args)
	if err != nil {
		t.Fatalf("%s %s(%s): %v", c.Name, name, args, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%s の返り値が JSON ではありません: %q", name, text)
	}
	return m
}

func errAs(t *testing.T, f *toolstest.Fixture, c auth.Client, name, args string) *apperr.Error {
	t.Helper()
	_, err := callAs(t, f, c, toolkit.EndpointOps, name, args)
	if err == nil {
		t.Fatalf("%s %s(%s): 成功しましたが失敗を期待しました", c.Name, name, args)
	}
	return apperr.As(err)
}

// twoSessions は Fixture をセッション 0（5 ペイン）・1（3 列×2 段の 6 ペイン）の 2 セッション構成にする。
func twoSessions(t *testing.T) *toolstest.Fixture {
	t.Helper()
	f := toolstest.New(t)
	f.UseTwoSessions()
	return f
}

func hostAuthExpired(name string) host.Auth { return host.Auth{Agent: name, Status: host.AuthExpired} }

func posOf(t *testing.T, m map[string]any) []string {
	t.Helper()
	var out []string
	for _, p := range m["panes"].([]any) {
		out = append(out, p.(map[string]any)["pos"].(string))
	}
	return out
}

// 受け入れ 1・2: kana と natsumi は互いのセッションを読めも操作もできない。省略時は自分のセッション。
func TestSessionIsolationBetweenTokens(t *testing.T) {
	f := twoSessions(t)

	m := mustAs(t, f, natsumi, "panes_status", `{}`)
	if got, want := posOf(t, m), []string{"T1", "T2", "T3", "B1", "B2", "B3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("natsumi の panes_status の位置 = %v, want %v（0:T1 のような接頭辞は付かない）", got, want)
	}
	layout := m["layout"].(map[string]any)
	if layout["rows"] != float64(2) || !reflect.DeepEqual(layout["cols"], []any{float64(3), float64(3)}) {
		t.Errorf("layout はセッション 1 で計算し直す: %v", layout)
	}
	m = mustAs(t, f, kana, "panes_status", `{}`)
	if got := posOf(t, m); !reflect.DeepEqual(got, []string{"T1", "T2", "B1", "B2", "B3"}) {
		t.Errorf("kana の位置 = %v", got)
	}
	if layout := m["layout"].(map[string]any); !reflect.DeepEqual(layout["cols"], []any{float64(2), float64(3)}) {
		t.Errorf("kana の layout = %v", layout)
	}

	for _, c := range []struct {
		client auth.Client
		tool   string
		args   string
	}{
		{kana, "panes_status", `{"session":"1"}`},
		{kana, "pane_tail", `{"session":"1","pane":"T1"}`},
		{kana, "ask_card", `{"session":"1","pane":"T1"}`},
		{kana, "report_read", `{"session":"1","pane":"T1"}`},
		{kana, "note_get", `{"session":"1","pane":"T1"}`},
		{kana, "report", `{"session":"1"}`},
		{kana, "report", `{"session":"1","format":"json"}`},
		{kana, "host_status", `{"session":"1"}`},
		{kana, "dispatch", `{"session":"1","pane":"T1","agent":"omp","task":"t","profile":"fix"}`},
		{kana, "send_text", `{"session":"1","pane":"T1","text":"x"}`},
		{kana, "send_escape", `{"session":"1","pane":"T1","confirm":true}`},
		{kana, "ask_pick", `{"session":"1","pane":"T1","n":1}`},
		{kana, "pane_start", `{"session":"1","pane":"B1","agent":"omp","cwd":"` + f.Root + `","title":"t"}`},
		{kana, "pane_close", `{"session":"1","pane":"T1"}`},
		{kana, "note_set", `{"session":"1","pane":"T1","set":{"from":"x"}}`},
		{kana, "note_log", `{"session":"1","pane":"T1","text":"x"}`},
		{natsumi, "panes_status", `{"session":"0"}`},
		{natsumi, "pane_tail", `{"session":"0","pane":"T1"}`},
		{natsumi, "dispatch", `{"session":"0","pane":"T1","agent":"omp","task":"t","profile":"fix"}`},
	} {
		e := errAs(t, f, c.client, c.tool, c.args)
		wantSession := "1"
		if c.client.Name == "natsumi" {
			wantSession = "0"
		}
		if e.Code != apperr.SessionForbidden || e.Details["session"] != wantSession || e.Message() != "このセッションは許可されていません" {
			t.Errorf("%s %s(%s) = %s %v", c.client.Name, c.tool, c.args, e.Code, e.Details)
		}
	}
	if f.Ops.Called() {
		t.Errorf("許可外なのに操作が呼ばれました: %v", f.Ops.Calls)
	}
	if f.Registry.TailCalls != 0 {
		t.Errorf("許可外なのに画面を読みました")
	}
}

// 許可外のペインは tmux ID・接頭辞つきの位置・固定名で直接指しても session_forbidden。
// session を省略しても、許可内のセッションの外を指せば同じ。
func TestForbiddenPaneByIDOrGlobalName(t *testing.T) {
	f := twoSessions(t)
	for _, pane := range []string{"%11", "1:T1", "1:t1"} {
		for _, sess := range []string{"", `"session":"0",`} {
			args := `{` + sess + `"pane":"` + pane + `"}`
			if e := errAs(t, f, kana, "pane_tail", args); e.Code != apperr.SessionForbidden || e.Details["session"] != "1" {
				t.Errorf("kana pane_tail(%s) = %s %v", args, e.Code, e.Details)
			}
		}
	}
	// 許可外のペインの存在・中身は details に出ない。
	e := errAs(t, f, kana, "pane_tail", `{"pane":"%11"}`)
	if len(e.Details) != 1 {
		t.Errorf("details = %v", e.Details)
	}
	if f.Registry.TailCalls != 0 {
		t.Error("許可外なのに画面を読みました")
	}
}

// 受け入れ 3・4: 許可が複数のトークンは session 省略で session_required、config 外の名前は invalid_args。
func TestSessionRequiredAndInvalid(t *testing.T) {
	f := twoSessions(t)
	for _, c := range []struct{ tool, args string }{
		{"panes_status", `{}`}, {"report", `{}`}, {"host_status", `{}`}, {"pane_tail", `{"pane":"T1"}`},
		{"dispatch", `{"pane":"T1","agent":"omp","task":"t","profile":"fix"}`}, {"note_set", `{"pane":"T1","set":{"from":"x"}}`},
	} {
		e := errAs(t, f, boss, c.tool, c.args)
		if e.Code != apperr.SessionRequired || e.Details["allowed_count"] != 2 || e.Message() != "session を指定してください" {
			t.Errorf("%s(%s) = %s %v", c.tool, c.args, e.Code, e.Details)
		}
		if len(e.Details) != 1 {
			t.Errorf("許可の一覧が details に出ています: %v", e.Details)
		}
	}
	for _, c := range []struct{ tool, args string }{
		{"panes_status", `{"session":"2"}`}, {"report", `{"session":"2"}`}, {"host_status", `{"session":"2"}`},
		{"pane_tail", `{"session":"2","pane":"T1"}`}, {"dispatch", `{"session":"2","pane":"T1","agent":"omp","task":"t","profile":"fix"}`},
	} {
		e := errAs(t, f, boss, c.tool, c.args)
		if e.Code != apperr.InvalidArgs || e.Details["field"] != "session" {
			t.Errorf("%s(%s) = %s %v", c.tool, c.args, e.Code, e.Details)
		}
	}
	// 許可が 0 になったトークン（tokens.toml の名前が config から外れた）も session_required。
	stale := auth.Client{Name: "stale", Scope: auth.ScopeOps, AllowedSessions: []string{"9"}}
	if e := errAs(t, f, stale, "panes_status", `{}`); e.Code != apperr.SessionRequired || e.Details["allowed_count"] != 0 {
		t.Errorf("許可 0: %s %v", e.Code, e.Details)
	}
	if e := errAs(t, f, stale, "panes_status", `{"session":"0"}`); e.Code != apperr.SessionForbidden {
		t.Errorf("許可 0 で明示: %s %v", e.Code, e.Details)
	}
}

// 複数許可のトークンは session を付ければ両方触れる。pane は session の中で解決する（同じ T1 でもセッションごとに別のペイン）。
func TestMultiAllowedResolvesPaneInsideSession(t *testing.T) {
	f := twoSessions(t)
	for _, c := range []struct{ session, wantID, wantPos string }{{"0", "%1", "T1"}, {"1", "%11", "1:T1"}} {
		m := mustAs(t, f, boss, "pane_tail", `{"session":"`+c.session+`","pane":"T1"}`)
		if m["pane"] != "T1" {
			t.Errorf("セッション %s の pane = %v（接頭辞なしの位置を返す）", c.session, m["pane"])
		}
		mustAs(t, f, boss, "send_text", `{"session":"`+c.session+`","pane":"T1","text":"x"}`)
		// 最終報告のファイル名には全体の位置（セッション 1 は 1:T1 → 1_T1.md）が使われる。
		if ref := f.Ops.LastRef; ref.ID != c.wantID || ref.Pos != c.wantPos || ref.Local != "T1" {
			t.Errorf("セッション %s の T1 = %+v, want ID=%s Pos=%s", c.session, ref, c.wantID, c.wantPos)
		}
	}
	// 接頭辞つき・固定名も、session と一致するときだけ受け付ける。
	mustAs(t, f, boss, "pane_tail", `{"session":"1","pane":"1:B2"}`)
	mustAs(t, f, boss, "pane_tail", `{"session":"1","pane":"%15"}`)
	// session と食い違う pane は invalid_args(pane)（許可内でも黙って切り替えない）。
	for _, args := range []string{`{"session":"0","pane":"1:B2"}`, `{"session":"0","pane":"%15"}`} {
		if e := errAs(t, f, boss, "pane_tail", args); e.Code != apperr.InvalidArgs || e.Details["field"] != "pane" {
			t.Errorf("pane_tail(%s) = %s %v", args, e.Code, e.Details)
		}
	}
	// 許可内で解決できない位置だけが pane_not_found。
	if e := errAs(t, f, boss, "pane_tail", `{"session":"1","pane":"R9"}`); e.Code != apperr.PaneNotFound {
		t.Errorf("R9: %s", e.Code)
	}
	// session 1 には B3 まである。session 0 は B3 まで（5 ペイン）で T3 は無い。
	if e := errAs(t, f, boss, "pane_tail", `{"session":"0","pane":"T3"}`); e.Code != apperr.PaneNotFound {
		t.Errorf("セッション 0 の T3: %s", e.Code)
	}
}

// 受け入れ 5: host_status の負荷などはホスト全体、agents・tmux_session だけ session で絞る。
func TestHostStatusSessionScope(t *testing.T) {
	f := twoSessions(t)
	k := mustAs(t, f, kana, "host_status", `{}`)
	n := mustAs(t, f, natsumi, "host_status", `{}`)
	for _, key := range []string{"load", "cpus", "mem_total_gb", "mem_free_gb", "mem", "cpu", "swap", "disk", "uptime", "usage", "omp", "update"} {
		if !reflect.DeepEqual(k[key], n[key]) {
			t.Errorf("%s がトークンで違います: %v / %v", key, k[key], n[key])
		}
	}
	if k["load"] == nil || k["cpus"] != float64(32) {
		t.Errorf("負荷が見えません: %v", k)
	}
	if want := map[string]any{"omp": float64(2), "claude": float64(1), "codex": float64(0)}; !reflect.DeepEqual(k["agents"], want) {
		t.Errorf("kana の agents = %v", k["agents"])
	}
	if want := map[string]any{"omp": float64(3), "claude": float64(1), "codex": float64(1)}; !reflect.DeepEqual(n["agents"], want) {
		t.Errorf("natsumi の agents = %v", n["agents"])
	}
	// 明示した session でも負荷は全体。許可外でも負荷は見えない（session 自体が許可外なので拒否）。
	if got := mustAs(t, f, boss, "host_status", `{"session":"1"}`); got["load"] == nil || !reflect.DeepEqual(got["agents"], n["agents"]) {
		t.Errorf("boss session=1: %v", got)
	}
	f.Registry.Snap.Panes = f.Registry.Snap.Panes[:5] // セッション 1 のペインが無い
	if got := mustAs(t, f, natsumi, "host_status", `{}`); got["tmux_session"] != false || got["load"] == nil {
		t.Errorf("セッションが無いとき: %v", got)
	}
}

// 実際のスナップショットと同じ形の固定名（Local・GlobalPos とも固定名・Fixed）は、別セッションの文脈では
// 許可外なら session_forbidden、許可内なら invalid_args(pane)。セッション内の位置のように「見つからない」にはならない。
func TestFixedNameAcrossSessions(t *testing.T) {
	f := twoSessions(t)
	for i := range f.Registry.Snap.Panes {
		if p := &f.Registry.Snap.Panes[i]; p.ID == "%13" {
			p.Pos, p.GlobalPos, p.Local, p.Fixed = "review", "review", "review", true
		}
	}
	if m := mustAs(t, f, natsumi, "pane_tail", `{"pane":"review"}`); m["pane"] != "review" {
		t.Errorf("natsumi の review = %v", m)
	}
	if e := errAs(t, f, kana, "pane_tail", `{"pane":"review"}`); e.Code != apperr.SessionForbidden || e.Details["session"] != "1" {
		t.Errorf("kana の review: %s %v", e.Code, e.Details)
	}
	if e := errAs(t, f, boss, "pane_tail", `{"session":"0","pane":"review"}`); e.Code != apperr.InvalidArgs || e.Details["field"] != "pane" {
		t.Errorf("boss session=0 の review: %s %v", e.Code, e.Details)
	}
}

// 認証を通っていない（名前の無い）Client は、AllowedSessions が nil でも何も許可されない（fail-closed）。
func TestAnonymousClientGetsNothing(t *testing.T) {
	f := twoSessions(t)
	anon := auth.Client{Scope: auth.ScopeOps}
	if e := errAs(t, f, anon, "pane_tail", `{"session":"0","pane":"T1"}`); e.Code != apperr.SessionForbidden {
		t.Errorf("名前の無い Client: %s", e.Code)
	}
	if got := sessionsOf(t, mustAs(t, f, anon, "sessions_list", `{}`)); len(got) != 0 {
		t.Errorf("sessions_list = %v", got)
	}
}
