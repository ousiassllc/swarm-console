package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// pane_start: シェルのペインで cd → 起動コマンドを打ち、idle になるまで待つ。タイトルは接頭辞つきで tmux に設定される。
func TestPaneStart(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	for _, name := range []string{"omp", "claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			id := h.NewShell()
			res := h.Ops().Map("pane_start", map[string]any{"pane": id, "agent": name, "cwd": h.Work, "title": "テスト " + name})
			if res["ok"] != true || res["state"] != "idle" || res["agent"] != name || res["cwd"] != h.Work || res["title"] != "agent: テスト "+name {
				t.Fatalf("pane_start = %v", res)
			}
			p, _ := h.Pane(id)
			if p.Agent == nil || *p.Agent != name || p.State != "idle" || p.Title != "agent: テスト "+name || p.Cwd != h.Work {
				t.Errorf("panes_status = %+v", p)
			}
			if got := strings.TrimSpace(h.TM.Run("display-message", "-p", "-t", id, "#{pane_title}")); got != "agent: テスト "+name {
				t.Errorf("tmux の pane_title = %q", got)
			}
		})
	}
}

// pane_close: 終了コマンド → cd ~ → clear → タイトル消し。確かめられたあと、メモを履歴へ、最終報告を archive/ へ移す。
func TestPaneClose(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewShell()
	h.Ops().Must("pane_start", map[string]any{"pane": id, "agent": "omp", "cwd": h.Work, "title": "閉じる"})
	pos := h.PosOf(id)
	h.Ops().Must("note_set", map[string]any{"pane": id, "set": map[string]any{"from": "agent-b", "repo": "ousiassllc/funid", "items": []string{"#982"}}})
	h.Ops().Must("note_log", map[string]any{"pane": id, "text": "着手した"})
	report := pane.ReportPath(h.Reports, pos)
	h.WriteFile(report, "最終報告\n")

	res := h.Ops().Map("pane_close", map[string]any{"pane": id})
	if res["ok"] != true || res["state"] != "shell" || res["note_archived"] != true ||
		!reflect.DeepEqual(res["steps"], []any{"/exit", "cd ~", "clear", "title"}) {
		t.Fatalf("pane_close = %v", res)
	}
	p, _ := h.Pane(id)
	if p.State != "shell" || p.Agent != nil || strings.HasPrefix(p.Title, "agent:") {
		t.Errorf("閉じたあとのペイン = %+v", p)
	}
	if note := h.Report().Map("note_get", map[string]any{"pane": id})["note"]; note != nil {
		t.Errorf("メモが残っている: %v", note)
	}
	hist := h.Report().Map("note_history", map[string]any{"repo": "ousiassllc/funid"})
	entries, _ := hist["entries"].([]any)
	if hist["total"] != float64(1) || len(entries) != 1 {
		t.Fatalf("note_history = %v", hist)
	}
	e := entries[0].(map[string]any)
	if e["pane_id"] != id || e["pos"] != pos || e["agent"] != "omp" || e["closed_by"] != "pane_close" || e["from"] != "agent-b" {
		t.Errorf("履歴 = %v", e)
	}
	if logs, _ := e["log"].([]any); len(logs) != 1 || logs[0].(map[string]any)["text"] != "着手した" {
		t.Errorf("履歴のログ = %v", e["log"])
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Errorf("最終報告が元の場所に残っている: %v", err)
	}
	moved, _ := filepath.Glob(filepath.Join(h.Reports, "archive", strings.ReplaceAll(pos, ":", "_")+"-*.md"))
	if len(moved) != 1 {
		t.Errorf("archive/ の最終報告 = %v", moved)
	}
	if data, err := os.ReadFile(moved[0]); err != nil || string(data) != "最終報告\n" {
		t.Errorf("archive の中身: %q %v", data, err)
	}

	// shell のペインは終了コマンドを飛ばす。
	res = h.Ops().Map("pane_close", map[string]any{"pane": id})
	if res["ok"] != true || res["note_archived"] != false || !reflect.DeepEqual(res["steps"], []any{"cd ~", "clear", "title"}) {
		t.Errorf("shell への pane_close = %v", res)
	}
}

func TestNotes(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	c, id := h.Ops(), h.P0
	set := c.Map("note_set", map[string]any{"pane": id, "set": map[string]any{
		"from": "agent-a", "repo": "ousiassllc/funid", "items": []string{"#981", "#982"}, "next": "レビュー待ち",
		"report_to": map[string]any{"kind": "agent", "name": "運用エージェント"},
	}})
	note := set["note"].(map[string]any)
	if set["ok"] != true || note["from"] != "agent-a" || note["next"] != "レビュー待ち" || note["updated_at"] == nil || note["log"] == nil {
		t.Fatalf("note_set = %v", set)
	}
	if got := h.Report().Map("note_get", map[string]any{"pane": id}); got["id"] != id || !reflect.DeepEqual(got["note"], note) {
		t.Errorf("note_get = %v", got)
	}
	// null でその項目を消す。ほかの項目は残る。
	note = c.Map("note_set", map[string]any{"pane": id, "set": map[string]any{"next": nil, "status": "review"}})["note"].(map[string]any)
	if _, has := note["next"]; has || note["status"] != "review" || note["from"] != "agent-a" {
		t.Errorf("更新後 = %v", note)
	}
	if got := c.Map("note_log", map[string]any{"pane": id, "text": "1 件目"})["log_size"]; got != float64(1) {
		t.Errorf("log_size = %v", got)
	}
	if got := c.Map("note_log", map[string]any{"pane": id, "text": "2 件目"})["log_size"]; got != float64(2) {
		t.Errorf("log_size = %v", got)
	}
	logs := h.Report().Map("note_get", map[string]any{"pane": id})["note"].(map[string]any)["log"].([]any)
	if len(logs) != 2 || logs[1].(map[string]any)["text"] != "2 件目" {
		t.Errorf("log = %v", logs)
	}
	// 位置でも ID でも同じメモ。
	pos := h.PosOf(id)
	if got := h.Report().Map("note_get", map[string]any{"pane": pos})["id"]; got != id {
		t.Errorf("位置 %s の ID = %v", pos, got)
	}
	for _, args := range []map[string]any{
		{"pane": id, "set": map[string]any{"log": []any{}}},
		{"pane": id, "set": map[string]any{"updated_at": "x"}},
		{"pane": id, "set": map[string]any{"nope": "x"}},
		{"pane": id, "set": map[string]any{"items": "文字列"}},
		{"pane": id},
	} {
		if code := c.Call("note_set", args).Code(t); code != "invalid_args" {
			t.Errorf("note_set %v: %s", args["set"], code)
		}
	}
	for _, text := range []string{"", "改行\nあり", strings.Repeat("あ", 501)} {
		if code := c.Call("note_log", map[string]any{"pane": id, "text": text}).Code(t); code != "invalid_args" {
			t.Errorf("note_log %.10q: %s", text, code)
		}
	}
	for _, lim := range []any{0, 101} {
		if code := h.Report().Call("note_history", map[string]any{"limit": lim}).Code(t); code != "invalid_args" {
			t.Errorf("note_history limit=%v: %s", lim, code)
		}
	}
	if got := h.Report().Map("note_history", map[string]any{"repo": "なし"}); got["total"] != float64(0) || len(got["entries"].([]any)) != 0 {
		t.Errorf("履歴なし: %v", got)
	}
}
