package integration

import (
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

func layoutWarning(s harness.Status) (string, bool) {
	for _, w := range s.Warnings {
		if w.Code == "layout_changed" {
			return w.Message, true
		}
	}
	return "", false
}

// AC-3: ペインを増やす・減らす・別セッションを足したあとの panes_status に、新しい位置と layout_changed の警告が出て、
// メモは tmux ID に結び付いたままずれない（位置名が変わっても同じペインに付く）。
// サンプラーは止め（NoSampler）、変更を見つけたスナップショットを呼び出し自身が作るので、警告を取りこぼさない。
func TestAC3LayoutChangeKeepsNotes(t *testing.T) {
	h := harness.Start(t, harness.Options{
		Width: 250, Height: 85, NoSampler: true,
		Config: map[string]string{"sessions": `["main", "extra"]`, "default_session": `"main"`, "layout_warning_ttl": `"10m"`},
	})
	a := h.P0
	h.Ops().Must("note_set", map[string]any{"session": "main", "pane": a, "set": map[string]any{"from": "agent-b", "repo": "ousiassllc/funid", "items": []string{"#982"}}})
	// 基準のスナップショット。config の sessions は 2 つ（トークンは両方を許可）なので session を付ける。
	// main の応答には、まだ無い extra の session_missing も載らない（警告はセッションごと）。
	st := h.StatusOf("main")
	if _, warned := layoutWarning(st); warned || len(st.Panes) != 1 || len(st.Warnings) != 0 || st.Panes[0].Pos != "T1" {
		t.Fatalf("基準: %+v", st)
	}
	if ex := h.StatusOf("extra"); len(ex.Panes) != 0 || len(ex.Warnings) != 1 || ex.Warnings[0].Code != "session_missing" {
		t.Fatalf("extra の基準: %+v", ex)
	}

	// 増やす: a の左に新しいペイン b を足す → b が T1、a が T2 にずれる。
	b := strings.TrimSpace(h.TM.Run("split-window", "-hb", "-d", "-P", "-F", "#{pane_id}", "-t", a))
	var msg string
	h.Eventually("ペインを増やした警告", func() bool {
		var ok bool
		msg, ok = layoutWarning(h.StatusOf("main"))
		return ok
	})
	st = h.StatusOf("main")
	pos := map[string]string{}
	for _, p := range st.Panes {
		pos[p.ID] = p.Pos
	}
	if pos[b] != "T1" || pos[a] != "T2" {
		t.Fatalf("位置 = %v, want b=T1 a=T2", pos)
	}
	for _, want := range []string{"T1 が追加されました", "T2 が T1→T2 に変わりました"} {
		if !strings.Contains(msg, want) {
			t.Errorf("警告 %q に %q がありません", msg, want)
		}
	}
	// メモは a（ID）に付いたまま。新しい位置 T2 でも古い位置 T1 でもなく、ID で追える。
	got := h.Report().Map("note_get", map[string]any{"session": "main", "pane": "T2"})
	if got["id"] != a || got["note"] == nil || got["note"].(map[string]any)["from"] != "agent-b" {
		t.Errorf("T2 のメモ = %v", got)
	}
	if got := h.Report().Map("note_get", map[string]any{"session": "main", "pane": "T1"}); got["id"] != b || got["note"] != nil {
		t.Errorf("T1（新しいペイン）にメモが移っている: %v", got)
	}
	for _, p := range st.Panes {
		if (p.ID == a) != (p.Note != nil) {
			t.Errorf("panes_status の note の付き先が違う: %+v", p)
		}
	}

	// 減らす: b を消す → a が T1 に戻る。メモはそのまま。
	h.TM.Run("kill-pane", "-t", b)
	h.Eventually("ペインを減らした警告", func() bool {
		m, ok := layoutWarning(h.StatusOf("main"))
		msg = m
		return ok && strings.Contains(m, "削除されました")
	})
	if !strings.Contains(msg, "T2 が削除されました") && !strings.Contains(msg, "T1 が削除されました") {
		t.Errorf("警告 = %q", msg)
	}
	if p := h.StatusOf("main").Panes; len(p) != 1 || p[0].Pos != "T1" || p[0].Note == nil || p[0].Note["from"] != "agent-b" {
		t.Errorf("a = %+v", p)
	}

	// 別のセッションを足す → extra の位置は extra の中で T1 から（接頭辞なし）。layout_changed は extra の応答だけに、
	// extra の位置で出る。main の応答には出ない（main 自身の警告は main の位置だけ）。
	extra := h.TM.NewSession("extra", 250, 85, "")
	h.Eventually("別セッションの警告", func() bool {
		m, ok := layoutWarning(h.StatusOf("extra"))
		msg = m
		return ok && strings.Contains(m, "T1 が追加されました")
	})
	if strings.Contains(msg, "extra:") {
		t.Errorf("extra の警告に接頭辞が付いている: %q", msg)
	}
	ex := h.StatusOf("extra")
	if len(ex.Panes) != 1 || ex.Panes[0].ID != extra || ex.Panes[0].Pos != "T1" {
		t.Errorf("extra のペイン = %+v", ex.Panes)
	}
	mainSt := h.StatusOf("main")
	if m, _ := layoutWarning(mainSt); strings.Contains(m, "追加されました") && strings.Contains(m, extra) {
		t.Errorf("main の警告に extra が混ざっている: %q", m)
	}
	if len(mainSt.Panes) != 1 || mainSt.Panes[0].ID != a || mainSt.Panes[0].Pos != "T1" || mainSt.Panes[0].Note == nil {
		t.Errorf("既定セッションの a が動いた: %+v", mainSt.Panes)
	}
	// session を省略すると、許可が 2 つなので session_required。
	if code := h.Ops().Call("panes_status", nil).Code(t); code != "session_required" {
		t.Errorf("session 省略: %s", code)
	}
}

// 警告は layout_warning_ttl が過ぎると消える（時計は実時間。短い ttl に設定し、上限つきで待つ）。
func TestAC3LayoutWarningExpires(t *testing.T) {
	h := harness.Start(t, harness.Options{NoSampler: true, Config: map[string]string{"layout_warning_ttl": `"2s"`}})
	h.Status() // 基準
	h.NewWindow("")
	// 変更を見つけたスナップショットを作るのはこの呼び出し自身なので、最初に出た応答に警告が載る。
	h.Eventually("警告が出る", func() bool { _, warned := layoutWarning(h.Status()); return warned })
	h.Eventually("警告が消える", func() bool { _, warned := layoutWarning(h.Status()); return !warned })
}
