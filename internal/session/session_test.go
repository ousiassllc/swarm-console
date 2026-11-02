package session_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/session"
)

func str(s string) *string { return &s }

func TestAllowed(t *testing.T) {
	cfg := []string{"0", "1", "2"}
	for _, c := range []struct {
		name    string
		granted []string
		want    []string
	}{
		{"nil は config の全部", nil, []string{"0", "1", "2"}},
		{"config の順を保つ", []string{"2", "0"}, []string{"0", "2"}},
		{"config に無い名前は落ちる", []string{"1", "x"}, []string{"1"}},
		{"共通部分が空", []string{"x"}, []string{}},
	} {
		if got := session.Allowed(c.granted, cfg); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPick(t *testing.T) {
	cfg := []string{"0", "1", "2"}
	for _, c := range []struct {
		name    string
		allowed []string
		arg     *string
		want    string
		code    apperr.Code
		detail  string // details のキー（期待値は下で個別に確認）
	}{
		{"許可 1 つ・省略 → そのセッション", []string{"0"}, nil, "0", "", ""},
		{"許可 2 つ・省略 → session_required", []string{"0", "1"}, nil, "", apperr.SessionRequired, "allowed_count"},
		{"許可 0・省略 → session_required", nil, nil, "", apperr.SessionRequired, "allowed_count"},
		{"明示・許可内", []string{"0", "1"}, str("1"), "1", "", ""},
		{"明示・config 外 → invalid_args", []string{"0"}, str("9"), "", apperr.InvalidArgs, "field"},
		{"明示・config 内だが許可外 → session_forbidden", []string{"0"}, str("1"), "", apperr.SessionForbidden, "session"},
		{"空文字は config に無い名前", []string{"0"}, str(""), "", apperr.InvalidArgs, "field"},
	} {
		got, err := session.Pick(c.allowed, cfg, c.arg)
		if c.code == "" {
			if err != nil || got != c.want {
				t.Errorf("%s: got %q, %v", c.name, got, err)
			}
			continue
		}
		e := apperr.As(err)
		if err == nil || e.Code != c.code {
			t.Errorf("%s: err = %v, want %s", c.name, err, c.code)
			continue
		}
		if _, ok := e.Details[c.detail]; !ok {
			t.Errorf("%s: details = %v, want key %s", c.name, e.Details, c.detail)
		}
	}
	// session_required は許可の一覧を返さず個数だけ。
	_, err := session.Pick([]string{"0", "1"}, cfg, nil)
	if d := apperr.As(err).Details; len(d) != 1 || d["allowed_count"] != 2 {
		t.Errorf("session_required の details = %v", d)
	}
	// session_forbidden は引数の名前を返す。
	_, err = session.Pick([]string{"0"}, cfg, str("1"))
	if d := apperr.As(err).Details; d["session"] != "1" {
		t.Errorf("session_forbidden の details = %v", d)
	}
}

// snap は 2 セッション（0=既定・1）の 3×2 を模したスナップショット。1 のペインの全体の位置は 1:T1…。
func snap() *pane.Snapshot {
	mk := func(id, session, global, local string) pane.PaneState {
		return pane.PaneState{ID: id, Session: session, Pos: global, GlobalPos: global, Local: local}
	}
	// 固定名（pane_names）は、実際のスナップショットと同じく Local も固定名で、Fixed が立つ。
	fixed := func(id, session, name string) pane.PaneState {
		return pane.PaneState{ID: id, Session: session, Pos: name, GlobalPos: name, Local: name, Fixed: true}
	}
	return &pane.Snapshot{Panes: []pane.PaneState{
		mk("%1", "0", "T1", "T1"), mk("%2", "0", "T2", "T2"),
		mk("%11", "1", "1:T1", "T1"), mk("%12", "1", "1:T2", "T2"), fixed("%13", "1", "review"),
	}}
}

func TestResolveInSession(t *testing.T) {
	both := []string{"0", "1"}
	for _, c := range []struct {
		name    string
		allowed []string
		in, arg string
		wantID  string
		wantPos string // 最終報告の名前に使う接頭辞つきの位置
		wantLoc string
		code    apperr.Code
		field   string
	}{
		{"セッション内の位置（0）", []string{"0"}, "0", "T1", "%1", "T1", "T1", "", ""},
		{"セッション内の位置（1）は別のペイン", both, "1", "T1", "%11", "1:T1", "T1", "", ""},
		{"大文字小文字は区別しない", both, "1", "t2", "%12", "1:T2", "T2", "", ""},
		{"tmux ID", both, "1", "%12", "%12", "1:T2", "T2", "", ""},
		{"接頭辞つきの位置（セッションと一致）", both, "1", "1:T2", "%12", "1:T2", "T2", "", ""},
		{"<セッション>:<セッション内の位置>", both, "1", "1:t2", "%12", "1:T2", "T2", "", ""},
		{"固定名", both, "1", "review", "%13", "review", "review", "", ""},
		{"許可外セッションの tmux ID → session_forbidden", []string{"0"}, "0", "%11", "", "", "", apperr.SessionForbidden, ""},
		{"許可外セッションの接頭辞つきの位置 → session_forbidden", []string{"0"}, "0", "1:T1", "", "", "", apperr.SessionForbidden, ""},
		{"許可外の固定名 → session_forbidden", []string{"0"}, "0", "review", "", "", "", apperr.SessionForbidden, ""},
		{"許可内だが別セッションの ID → invalid_args(pane)", both, "0", "%11", "", "", "", apperr.InvalidArgs, "pane"},
		{"許可内だが別セッションの接頭辞つき → invalid_args(pane)", both, "0", "1:T2", "", "", "", apperr.InvalidArgs, "pane"},
		{"許可内だが別セッションの固定名 → invalid_args(pane)", both, "0", "review", "", "", "", apperr.InvalidArgs, "pane"},
		{"存在しない位置 → pane_not_found", both, "0", "B9", "", "", "", apperr.PaneNotFound, ""},
		{"許可外に存在しない位置も pane_not_found（存在の有無を漏らす経路は ID・名前の一致だけ）", []string{"0"}, "0", "9:T1", "", "", "", apperr.PaneNotFound, ""},
	} {
		ref, err := session.Resolve(snap(), c.allowed, c.in, c.arg)
		if c.code == "" {
			if err != nil || ref.ID != c.wantID || ref.Pos != c.wantPos || ref.Local != c.wantLoc {
				t.Errorf("%s: ref=%+v err=%v", c.name, ref, err)
			}
			continue
		}
		e := apperr.As(err)
		if err == nil || e.Code != c.code {
			t.Errorf("%s: err = %v, want %s", c.name, err, c.code)
			continue
		}
		if c.field != "" && e.Details["field"] != c.field {
			t.Errorf("%s: details = %v", c.name, e.Details)
		}
	}
}

// 長い引数は details.pane に 100 字までしか載せない（クライアントの入力をそのまま返さない）。
func TestResolveNotFoundEchoIsClipped(t *testing.T) {
	_, err := session.Resolve(snap(), []string{"0", "1"}, "0", strings.Repeat("x", 300))
	e := apperr.As(err)
	if shown, _ := e.Details["pane"].(string); e.Code != apperr.PaneNotFound || len(shown) != 100 {
		t.Fatalf("%s details=%v", e.Code, e.Details)
	}
}

// 許可外のペインを指す引数は、実在してもしなくても session_forbidden か pane_not_found のどちらかに固定され、
// 許可外のペインの中身（位置・タイトルなど）は details に出ない。
func TestResolveForbiddenDetailsOnlyName(t *testing.T) {
	_, err := session.Resolve(snap(), []string{"0"}, "0", "%11")
	d := apperr.As(err).Details
	if len(d) != 1 || d["session"] != "1" {
		t.Fatalf("details = %v", d)
	}
}

// resources の識別子（in が空）: 許可されたセッション全体から、接頭辞つきの位置か tmux ID で探す。
func TestResolveAcrossAllowed(t *testing.T) {
	both := []string{"0", "1"}
	for arg, want := range map[string]string{"T1": "%1", "1:T1": "%11", "%13": "%13", "review": "%13"} {
		ref, err := session.Resolve(snap(), both, "", arg)
		if err != nil || ref.ID != want {
			t.Errorf("%q: ref=%+v err=%v, want %s", arg, ref, err, want)
		}
	}
	// セッション内の位置（T3 など、全体の名前でないもの）は、セッションが決まらないので当たらない。
	if _, err := session.Resolve(snap(), both, "", "T3"); apperr.As(err).Code != apperr.PaneNotFound {
		t.Errorf("T3: %v", err)
	}
	if _, err := session.Resolve(snap(), []string{"0"}, "", "%11"); apperr.As(err).Code != apperr.SessionForbidden {
		t.Errorf("許可外の ID: %v", err)
	}
}
