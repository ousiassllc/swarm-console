package tooltests_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// Issue #120: task で組み立てた追加の指示は、dispatch と同じ条件で、全文を sent に返す。
func TestSendTextTaskBuildsFollowUpLikeDispatch(t *testing.T) {
	f := toolstest.New(t)
	f.Ops.Send = pane.SendResult{OK: true, State: agent.StateWorking, Accepted: true, Chars: 9}
	out, err := call(t, f, "send_text", `{"pane":"T1","task":"テストを足す","profile":"fix","extra_rules":["E"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		OK   bool   `json:"ok"`
		Sent string `json:"sent"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Sent == "" || res.Sent != f.Ops.LastText {
		t.Fatalf("sent は貼った本文と同じ全文のはず: out=%s / 貼った本文 %q", out, f.Ops.LastText)
	}
	if !strings.HasPrefix(res.Sent, "## 追加の指示") || strings.Contains(res.Sent, "{report_path}") {
		t.Fatalf("追加の指示の見出し・置換: %q", res.Sent)
	}
	// 同じペイン・同じ profile・extra_rules の dispatch と、見出し以外が一致する（条件の部分がそっくり同じ）。
	f.Ops.DispatchRes = pane.DispatchResult{OK: true, State: agent.StateWorking, Accepted: true, Sent: true}
	dout, err := call(t, f, "dispatch", `{"pane":"T1","agent":"omp","task":"テストを足す","profile":"fix","extra_rules":["E"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Sent string `json:"sent"`
	}
	if err := json.Unmarshal([]byte(dout), &d); err != nil {
		t.Fatal(err)
	}
	_, followConds, ok1 := strings.Cut(res.Sent, "\n\n## 条件\n")
	_, dispatchConds, ok2 := strings.Cut(d.Sent, "\n\n## 条件\n")
	if !ok1 || !ok2 || followConds != dispatchConds || followConds == "" {
		t.Fatalf("条件が dispatch と違う\nsend_text:\n%s\ndispatch:\n%s", res.Sent, d.Sent)
	}
}

func TestSendTextReturnsReaction(t *testing.T) {
	for _, tc := range []struct {
		reaction agent.Reaction
		basis    string
		want     string
	}{
		{agent.ReactionActed, agent.BasisUserMessage, `{"ok":true,"state":"working","accepted":true,"chars":2,"reaction":"acted","reaction_basis":"user_message"}`},
		{agent.ReactionIgnored, agent.BasisToolOutput, `{"ok":true,"state":"working","accepted":true,"chars":2,"reaction":"ignored","reaction_basis":"tool_output"}`},
		{agent.ReactionUnverified, agent.BasisNotFound, `{"ok":true,"state":"working","accepted":true,"chars":2,"reaction":"unverified","reaction_basis":"not_found"}`},
	} {
		f := toolstest.New(t)
		f.Ops.Send = pane.SendResult{OK: true, State: agent.StateWorking, Accepted: true, Chars: 2, Reaction: tc.reaction, ReactionBasis: tc.basis}
		got, err := call(t, f, "send_text", `{"pane":"T1","text":"追加"}`)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %s err=%v", tc.reaction, got, err)
		}
	}
}

func TestSendTextTaskRejectsBeforeSending(t *testing.T) {
	f := toolstest.New(t)
	for _, c := range []struct{ args, field string }{
		{`{"pane":"T1","task":"x","text":"y","profile":"fix"}`, "text"},
		{`{"pane":"T1","task":"x","file":"/tmp/a","profile":"fix"}`, "text"},
		{`{"pane":"T1","task":"x"}`, "profile"},
		{`{"pane":"T1","task":"x","profile":"nope"}`, "profile"},
		{`{"pane":"T1","task":"","profile":"fix"}`, "task"},
		{`{"pane":"T1","text":"x","profile":"fix"}`, "task"},
		{`{"pane":"T1","text":"x","extra_rules":["a"]}`, "task"},
	} {
		_, err := call(t, f, "send_text", c.args)
		if d := wantCode(t, err, apperr.InvalidArgs); d["field"] != c.field {
			t.Errorf("%s: details.field = %v, want %s", c.args, d["field"], c.field)
		}
	}
	_, err := call(t, f, "send_text", `{"pane":"T1","task":"`+strings.Repeat("あ", 8001)+`","profile":"fix"}`)
	wantCode(t, err, apperr.TextTooLong)
	_, err = call(t, f, "send_text", `{"pane":"T1","task":"x","profile":"fix","extra_rules":["a\nb"]}`)
	if d := wantCode(t, err, apperr.ForbiddenControlChar); d["field"] != "extra_rules" {
		t.Errorf("details.field = %v", d["field"])
	}
	if f.Ops.Called() {
		t.Error("拒否したのに操作が呼ばれました")
	}
}
