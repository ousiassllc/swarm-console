package tooltests_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// note_set・note_get で origin・started_by を読み書きでき、from とは別の項目として残る。
func TestNoteSetGetOriginStartedBy(t *testing.T) {
	f := toolstest.New(t)
	m := mustCall(t, f, "note_set", `{"pane":"T1","set":{"from":"ゆな（buddy-ai）","origin":"agent","started_by":"かな"}}`)
	if n, _ := m["note"].(map[string]any); n["from"] != "ゆな（buddy-ai）" || n["origin"] != "agent" || n["started_by"] != "かな" {
		t.Fatalf("note_set = %v", m)
	}
	mustCall(t, f, "note_set", `{"pane":"T1","set":{"origin":null}}`)
	m = mustCall(t, f, "note_get", `{"pane":"T1"}`)
	n, _ := m["note"].(map[string]any)
	if _, has := n["origin"]; has || n["started_by"] != "かな" || n["from"] != "ゆな（buddy-ai）" {
		t.Fatalf("origin だけが消えるはず: %v", n)
	}
	for _, args := range []string{
		`{"pane":"T1","set":{"origin":"owner"}}`,
		`{"pane":"T1","set":{"origin":1}}`,
	} {
		_, err := call(t, f, "note_set", args)
		if d := wantCode(t, err, apperr.InvalidArgs); d["field"] != "origin" {
			t.Errorf("%s: details = %v", args, d)
		}
	}
	_, err := call(t, f, "note_set", `{"pane":"T1","set":{"started_by":"`+strings.Repeat("a", 2001)+`"}}`)
	wantCode(t, err, apperr.InvalidArgs)
}

// dispatch の note でも書け、不正な origin は何も送らず保存もしない。
func TestDispatchNoteOrigin(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Sent: true}
	m := mustCall(t, f, "dispatch", `{"pane":"T1",`+dispatchOK+`,"note":{"origin":"agent","started_by":"かな"}}`)
	if m["note_saved"] != true {
		t.Fatalf("dispatch = %v", m)
	}
	if n, ok := f.Notes.Get("%1"); !ok || n.Origin != "agent" || n.StartedBy != "かな" {
		t.Fatalf("メモ = %+v", n)
	}

	f = toolstest.New(t)
	f.Ops.DispatchRes = pane.DispatchResult{OK: true}
	_, err := call(t, f, "dispatch", `{"pane":"T1",`+dispatchOK+`,"note":{"origin":"robot"}}`)
	wantCode(t, err, apperr.InvalidArgs)
	if f.Ops.LastDispatch.Text != "" {
		t.Errorf("事前検証で落ちたのに送った: %+v", f.Ops.LastDispatch)
	}
	if _, ok := f.Notes.Get("%1"); ok {
		t.Error("事前検証で落ちたのにメモを保存した")
	}
}

// note_history は origin・started_by でも絞れる（完全一致）。from の絞り込みは変わらない。
func TestNoteHistoryFilterOriginStartedBy(t *testing.T) {
	f := toolstest.New(t)
	seed := []struct{ id, pos, set string }{
		{"%1", "T1", `{"from":"ゆな","origin":"agent","started_by":"かな"}`},
		{"%2", "T2", `{"from":"ゆな","origin":"user"}`},
		{"%3", "B1", `{"from":"べつ","origin":"agent","started_by":"ゆな"}`},
	}
	for _, s := range seed {
		mustCall(t, f, "note_set", `{"pane":"`+s.pos+`","set":`+s.set+`}`)
		if _, err := f.Notes.Archive(context.Background(), noteMeta(s.id, s.pos), "pane_close"); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		args string
		want float64
	}{
		{`{"origin":"agent"}`, 2},
		{`{"origin":"user"}`, 1},
		{`{"started_by":"かな"}`, 1},
		{`{"origin":"agent","started_by":"ゆな"}`, 1},
		{`{"from":"ゆな"}`, 2},
		{`{"from":"ゆな","origin":"agent"}`, 1},
		{`{"origin":"owner"}`, 0},
	}
	for _, c := range cases {
		if h := mustCall(t, f, "note_history", c.args); h["total"] != c.want || float64(len(h["entries"].([]any))) != c.want {
			t.Errorf("%s: %v", c.args, h)
		}
	}
	h := mustCall(t, f, "note_history", `{"started_by":"かな"}`)
	e := h["entries"].([]any)[0].(map[string]any)
	if e["from"] != "ゆな" || e["origin"] != "agent" || e["started_by"] != "かな" {
		t.Errorf("履歴に from と 2 項目の両方が出ていません: %v", e)
	}
}

// /report（読み取り専用）には書き込みツールを出さない。note_history は origin・started_by を取る（FR-SAFE-6）。
func TestReportEndpointStaysReadOnlyForNotes(t *testing.T) {
	f := toolstest.New(t)
	for _, name := range []string{"note_set", "note_log", "dispatch"} {
		if _, ok := f.Tools.Lookup(tools.EndpointReport, name); ok {
			t.Errorf("/report に %s が出ている", name)
		}
	}
	if _, ok := f.Tools.Lookup(tools.EndpointReport, "note_history"); !ok {
		t.Error("/report に note_history がない")
	}
}
