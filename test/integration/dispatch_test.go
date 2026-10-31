package integration

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// expectedDispatch はサーバーとは別に render.Dispatch で組み立てた期待値（既定のプロファイル）。
func expectedDispatch(t *testing.T, h *harness.H, pos string, a map[string]any) string {
	t.Helper()
	profiles, err := config.LoadProfiles("")
	if err != nil {
		t.Fatal(err)
	}
	in := render.DispatchInput{
		Task: a["task"].(string), Profile: a["profile"].(string), Profiles: *profiles, ReportDir: h.Reports, Pos: pos,
	}
	in.Command, _ = a["command"].(string)
	in.ExtraRules, _ = a["extra_rules"].([]string)
	want, err := render.Dispatch(in)
	if err != nil {
		t.Fatal(err)
	}
	return want
}

func dispatchArgs(pane string) map[string]any {
	return map[string]any{
		"pane": pane, "agent": "omp", "command": "/issue-sweep #982 --single-pr --base main",
		"task": "#982 を直す\n再現手順は issue に書いてある", "profile": "fix", "extra_rules": []string{"追加条件 1", "追加条件 2"},
		"note": map[string]any{"from": "ふみ", "repo": "ousiassllc/funid", "items": []string{"#982"}},
	}
}

// dispatch（shell から）: 起動 → idle → 指示文を貼り付けて Enter。sent は render.Dispatch の期待と 1 文字一致し、
// 擬似エージェントに貼られた本文とも一致する。メモは一緒に保存される。
func TestDispatchFromShell(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewShell()
	args := dispatchArgs(id)
	args["cwd"], args["title"] = h.Work, "funid #982"
	res := h.Ops().Map("dispatch", args)
	want := expectedDispatch(t, h, h.PosOf(id), args)
	if res["ok"] != true || res["accepted"] != true || res["started"] != true || res["state"] != "working" ||
		res["note_saved"] != true || res["sent"] != want {
		t.Fatalf("dispatch = %v\nwant sent = %q", res, want)
	}
	ev := h.WaitEvents(id, "貼り付けと Enter", func(ev []fakeEvent) bool { return len(ev) >= 2 })
	if !reflect.DeepEqual(harness.Kinds(ev), []string{"PASTE", "SUBMIT"}) || ev[0].Text != want {
		t.Errorf("--log = %+v", ev)
	}
	p, _ := h.Pane(id)
	if p.Title != "kana: funid #982" || p.Agent == nil || *p.Agent != "omp" {
		t.Errorf("ペイン = %+v", p)
	}
	note := h.Report().Map("note_get", map[string]any{"pane": id})["note"].(map[string]any)
	if note["from"] != "ふみ" || note["repo"] != "ousiassllc/funid" {
		t.Errorf("note = %v", note)
	}
}

// dispatch（idle へ）: cwd・title は無視され、started は false。指示文の中の {report_path} は送り先の位置で決まる。
func TestDispatchToIdle(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewAgent("claude", "idle")
	h.WaitState(id, "idle")
	args := dispatchArgs(id)
	args["agent"], args["profile"] = "claude", "sweep"
	delete(args, "note")
	res := h.Ops().Map("dispatch", args)
	want := expectedDispatch(t, h, h.PosOf(id), args)
	if res["ok"] != true || res["started"] != false || res["state"] != "working" || res["note_saved"] != false || res["sent"] != want {
		t.Fatalf("dispatch = %v\nwant sent = %q", res, want)
	}
	ev := h.WaitEvents(id, "貼り付けと Enter", func(ev []fakeEvent) bool { return len(ev) >= 2 })
	if ev[0].Text != want {
		t.Errorf("貼られた本文 = %q, want %q", ev[0].Text, want)
	}
	if pos := h.PosOf(id); want != expectedDispatch(t, h, pos, args) || !strings.Contains(want, pane.ReportPath(h.Reports, pos)) {
		t.Errorf("指示文に最終報告のパス（%s）が入っていない:\n%s", pane.ReportPath(h.Reports, pos), want)
	}
}
