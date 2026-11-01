package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// 拒否の表。各行は 2 つの独立したクライアント（別トークン・別接続）で呼び、エラーコードと本文が 1 文字も違わないことを確かめる（AC-5）。
type rejection struct {
	name  string
	tool  string
	args  map[string]any
	code  apperr.Code
	field string // details.field があれば
}

func checkRejections(t *testing.T, h *harness.H, rows []rejection) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			a := h.Ops().Call(r.tool, r.args)
			b := h.OpsB().Call(r.tool, r.args)
			if got := a.Code(t); got != string(r.code) {
				t.Fatalf("code = %s, want %s\n%s", got, r.code, a.Text)
			}
			if a.Text != b.Text {
				t.Errorf("クライアントで応答が違う:\n%s\n%s", a.Text, b.Text)
			}
			e := a.Err(t).Error
			if e.Message != apperr.Message(r.code) {
				t.Errorf("message = %q, want %q", e.Message, apperr.Message(r.code))
			}
			if r.field != "" && e.Details["field"] != r.field {
				t.Errorf("details.field = %v, want %s", e.Details["field"], r.field)
			}
		})
	}
}

// 操作の拒否: 何も送らず何も保存しない。拒否のあとに擬似エージェントの --log が空で、メモも無いこと。
func TestOpsRejections(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	idle := h.NewAgent("omp", "idle")
	working := h.NewAgent("omp", "working")
	codexAsk := h.NewAgent("codex", "ask")
	ompAsk := h.NewAgent("omp", "ask")
	shell := h.NewShell()
	unknown := h.NewWindow("exec sleep 3000") // エージェントでもシェルでもないプロセス
	for id, state := range map[string]string{idle: "idle", working: "working", codexAsk: "asking", ompAsk: "asking", unknown: "unknown"} {
		h.WaitState(id, state)
	}

	outsideFile := filepath.Join(h.Outside, "secret.txt")
	h.WriteFile(outsideFile, "外のファイル")
	if err := os.Symlink(outsideFile, filepath.Join(h.Files, "link-out.txt")); err != nil {
		t.Fatal(err)
	}
	h.WriteFile(filepath.Join(h.Files, "ok.txt"), "ok")
	const ghToken = "ghp_" + "0123456789abcdefghijklmnopqrstuvwxyzAB" // ダミー（形式だけ）
	big := strings.Repeat("あ", 8001)

	checkRejections(t, h, []rejection{
		{"pane_start 許可外の cwd", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Outside, "title": "t"}, apperr.ForbiddenPath, "cwd"},
		{"pane_start 存在しない cwd", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": filepath.Join(h.Work, "none"), "title": "t"}, apperr.ForbiddenPath, "cwd"},
		{"pane_start 相対の cwd", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": "work", "title": "t"}, apperr.InvalidArgs, "cwd"},
		{"pane_start 制御文字の cwd", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work + "\n", "title": "t"}, apperr.InvalidArgs, "cwd"},
		{"pane_start 未知のエージェント", "pane_start", map[string]any{"pane": shell, "agent": "vim", "cwd": h.Work, "title": "t"}, apperr.InvalidArgs, "agent"},
		{"pane_start 制御文字のタイトル", "pane_start", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Work, "title": "a\x1b[31m"}, apperr.InvalidArgs, "title"},
		{"pane_start idle のペイン", "pane_start", map[string]any{"pane": idle, "agent": "omp", "cwd": h.Work, "title": "t"}, apperr.InvalidState, ""},
		{"dispatch 許可外の cwd", "dispatch", map[string]any{"pane": shell, "agent": "omp", "cwd": h.Outside, "title": "t", "task": "x", "profile": "fix"}, apperr.ForbiddenPath, "cwd"},
		{"dispatch note に秘密", "dispatch", map[string]any{"pane": idle, "agent": "omp", "task": "x", "profile": "fix", "note": map[string]any{"next": ghToken}}, apperr.SecretDetected, "next"},
		{"dispatch 未知のプロファイル", "dispatch", map[string]any{"pane": idle, "agent": "omp", "task": "x", "profile": "nope"}, apperr.InvalidArgs, "profile"},
		{"dispatch エージェント違い", "dispatch", map[string]any{"pane": idle, "agent": "codex", "task": "x", "profile": "fix"}, apperr.InvalidArgs, "agent"},
		{"dispatch command に改行", "dispatch", map[string]any{"pane": idle, "agent": "omp", "task": "x", "profile": "fix", "command": "/a\n/b"}, apperr.ForbiddenControlChar, "command"},
		{"dispatch 作業中のペイン", "dispatch", map[string]any{"pane": working, "agent": "omp", "task": "x", "profile": "fix"}, apperr.InvalidState, ""},
		{"dispatch 8000 字超", "dispatch", map[string]any{"pane": idle, "agent": "omp", "task": big, "profile": "fix"}, apperr.TextTooLong, ""},
		{"pane_close 作業中", "pane_close", map[string]any{"pane": working}, apperr.InvalidState, ""},
		{"pane_close 質問中", "pane_close", map[string]any{"pane": ompAsk}, apperr.InvalidState, ""},
		{"send_escape confirm なし", "send_escape", map[string]any{"pane": working}, apperr.ConfirmRequired, ""},
		{"send_escape confirm false", "send_escape", map[string]any{"pane": working, "confirm": false}, apperr.ConfirmRequired, ""},
		{"send_escape シェル", "send_escape", map[string]any{"pane": shell, "confirm": true}, apperr.InvalidState, ""},
		{"send_text ESC", "send_text", map[string]any{"pane": idle, "text": "a\x1b[31mb"}, apperr.ForbiddenControlChar, "text"},
		{"send_text NUL", "send_text", map[string]any{"pane": idle, "text": "a\x00b"}, apperr.ForbiddenControlChar, "text"},
		{"send_text CR", "send_text", map[string]any{"pane": idle, "text": "a\rb"}, apperr.ForbiddenControlChar, "text"},
		{"send_text 8001 字", "send_text", map[string]any{"pane": idle, "text": big}, apperr.TextTooLong, "text"},
		{"send_text text と file の両方", "send_text", map[string]any{"pane": idle, "text": "a", "file": filepath.Join(h.Files, "ok.txt")}, apperr.InvalidArgs, "text"},
		{"send_text 許可外の file", "send_text", map[string]any{"pane": idle, "file": outsideFile}, apperr.ForbiddenPath, "file"},
		{"send_text リンクで許可外へ抜ける file", "send_text", map[string]any{"pane": idle, "file": filepath.Join(h.Files, "link-out.txt")}, apperr.ForbiddenPath, "file"},
		{"send_text .. で抜ける file", "send_text", map[string]any{"pane": idle, "file": h.Files + "/../outside/secret.txt"}, apperr.ForbiddenPath, "file"},
		{"send_text 根の下の存在しない file", "send_text", map[string]any{"pane": idle, "file": filepath.Join(h.Files, "none.txt")}, apperr.FileUnreadable, "file"},
		{"send_text 質問中のペイン", "send_text", map[string]any{"pane": ompAsk, "text": "x"}, apperr.InvalidState, ""},
		{"send_text シェル", "send_text", map[string]any{"pane": shell, "text": "x"}, apperr.InvalidState, ""},
		{"send_text 不明なプロセス", "send_text", map[string]any{"pane": unknown, "text": "x"}, apperr.UnsupportedAgent, ""},
		{"send_escape 不明なプロセス", "send_escape", map[string]any{"pane": unknown, "confirm": true}, apperr.UnsupportedAgent, ""},
		{"pane_close 不明なプロセス", "pane_close", map[string]any{"pane": unknown}, apperr.UnsupportedAgent, ""},
		{"dispatch 不明なプロセス", "dispatch", map[string]any{"pane": unknown, "agent": "omp", "task": "x", "profile": "fix"}, apperr.UnsupportedAgent, ""},
		{"ask_pick not_asking", "ask_pick", map[string]any{"pane": idle, "n": 1}, apperr.NotAsking, ""},
		{"ask_pick n が範囲外", "ask_pick", map[string]any{"pane": ompAsk, "n": 9}, apperr.OptionOutOfRange, ""},
		{"ask_pick n=0", "ask_pick", map[string]any{"pane": ompAsk, "n": 0}, apperr.InvalidArgs, "n"},
		{"ask_pick n と text の両方", "ask_pick", map[string]any{"pane": ompAsk, "n": 1, "text": "x"}, apperr.InvalidArgs, "n"},
		{"ask_pick codex は自由入力できない", "ask_pick", map[string]any{"pane": codexAsk, "text": "自由入力"}, apperr.CustomNotAllowed, ""},
		{"ask_pick text に制御文字", "ask_pick", map[string]any{"pane": ompAsk, "text": "a\x00"}, apperr.ForbiddenControlChar, "text"},
		{"ask_pick シェル", "ask_pick", map[string]any{"pane": shell, "n": 1}, apperr.NotAsking, ""},
		{"ask_pick 不明なプロセス", "ask_pick", map[string]any{"pane": unknown, "n": 1}, apperr.UnsupportedAgent, ""},
		{"note_set 秘密", "note_set", map[string]any{"pane": idle, "set": map[string]any{"next": ghToken}}, apperr.SecretDetected, "next"},
		{"note_log 秘密", "note_log", map[string]any{"pane": idle, "text": "token=" + ghToken}, apperr.SecretDetected, ""},
		{"pane_not_found（操作）", "send_text", map[string]any{"pane": "Z9", "text": "x"}, apperr.PaneNotFound, ""},
		{"pane_not_found（ID）", "pane_close", map[string]any{"pane": "%9999"}, apperr.PaneNotFound, ""},
		{"未知の引数", "send_text", map[string]any{"pane": idle, "text": "x", "nope": 1}, apperr.InvalidArgs, "nope"},
		{"型違い", "send_escape", map[string]any{"pane": idle, "confirm": "yes"}, apperr.InvalidArgs, "confirm"},
	})

	for _, tool := range []string{"pane_tail", "ask_card", "report_read", "note_get"} {
		for _, args := range []map[string]any{{"pane": "Z9"}, {"pane": ""}} {
			code := apperr.PaneNotFound
			if args["pane"] == "" {
				code = apperr.InvalidArgs
			}
			checkRejections(t, h, []rejection{{"pane_not_found " + tool, tool, args, code, ""}})
		}
	}
	checkRejections(t, h, []rejection{
		{"ask_card 不明なプロセス", "ask_card", map[string]any{"pane": unknown}, apperr.UnsupportedAgent, ""},
	})

	// 拒否では何も送られず、何も保存されない。
	for _, id := range []string{idle, working, ompAsk, codexAsk, unknown} {
		if ev := h.Events(id); len(ev) != 0 {
			t.Errorf("%s: 拒否のあとに擬似エージェントへ何か届いた: %+v", id, ev)
		}
		if note := h.Ops().Map("note_get", map[string]any{"pane": id})["note"]; note != nil {
			t.Errorf("%s: 拒否のあとにメモが保存された: %v", id, note)
		}
	}
	if p, _ := h.Pane(shell); p.State != "shell" {
		t.Errorf("拒否のあとにシェルが %s", p.State)
	}
	if ev := h.Events(shell); len(ev) != 0 {
		t.Errorf("shell: %+v", ev)
	}
	// 秘密の値は応答にもログにも出ない。
	for _, r := range []harness.Result{
		h.Ops().Call("note_set", map[string]any{"pane": idle, "set": map[string]any{"next": ghToken}}),
		h.Ops().Call("note_log", map[string]any{"pane": idle, "text": ghToken}),
		h.Ops().Call("dispatch", map[string]any{"pane": idle, "agent": "omp", "task": "x", "profile": "fix", "note": map[string]any{"next": ghToken}}),
	} {
		if r.Code(t) != "secret_detected" || strings.Contains(r.Text, ghToken) {
			t.Errorf("秘密の拒否の応答: %s", r.Text)
		}
	}
	if strings.Contains(h.AuditLog(), ghToken) {
		t.Error("拒否した秘密の値がログに出ている")
	}
}
