package integration

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// send_text: 貼り付け本文が擬似エージェントの --log に 1 文字違わず届き、Enter で受理（working）になる。
func TestSendText(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	const text = "1行目 日本語 🙂\n  インデント付き 2行目\n\tタブ始まり 3行目"
	body := "ファイルの本文\n2行目\n"
	h.WriteFile(filepath.Join(h.Files, "body.txt"), body)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"text", map[string]any{"text": text}, text},
		{"file", map[string]any{"file": filepath.Join(h.Files, "body.txt")}, body},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := h.NewAgent("omp", "idle")
			h.WaitState(id, "idle")
			tt.args["pane"] = id
			res := h.Ops().Map("send_text", tt.args)
			if res["ok"] != true || res["accepted"] != true || res["state"] != "working" ||
				res["chars"] != float64(utf8.RuneCountInString(tt.want)) {
				t.Errorf("send_text = %v", res)
			}
			ev := h.WaitEvents(id, "貼り付けと Enter が届く", func(ev []fakeEvent) bool { return len(ev) >= 2 })
			if !reflect.DeepEqual(harness.Kinds(ev), []string{"PASTE", "SUBMIT"}) || ev[0].Text != tt.want {
				t.Errorf("--log = %+v", ev)
			}
		})
	}
}

// send_escape: 作業中のペインへ Esc を送って idle に戻し、実行を note_log に自動で残す。
func TestSendEscape(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewAgent("omp", "working")
	h.WaitState(id, "working")
	res := h.Ops().Map("send_escape", map[string]any{"pane": id, "confirm": true})
	if res["ok"] != true || res["state"] != "idle" || res["interrupted"] != true {
		t.Errorf("send_escape = %v", res)
	}
	ev := h.WaitEvents(id, "ESCAPE が届く", func(ev []fakeEvent) bool { return len(ev) > 0 })
	if !reflect.DeepEqual(harness.Kinds(ev), []string{"ESCAPE"}) {
		t.Errorf("--log = %+v", ev)
	}
	note := h.Report().Map("note_get", map[string]any{"pane": id})["note"].(map[string]any)
	logs, _ := note["log"].([]any)
	if len(logs) != 1 || !strings.Contains(logs[0].(map[string]any)["text"].(string), "Esc を送った（中断）") {
		t.Errorf("note.log = %v", note["log"])
	}
}
