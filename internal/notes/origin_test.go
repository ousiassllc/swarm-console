package notes

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// origin・started_by は from と別の項目で、部分更新と null 削除の決まりはほかの項目と同じ（FR-NOTE-4）。
func TestOriginStartedByMergeAndNullDelete(t *testing.T) {
	e := newEnv(t)
	n := e.set("%12", map[string]any{"from": "ゆな（buddy-ai）", "origin": "agent", "started_by": "かな"})
	if n.From != "ゆな（buddy-ai）" || n.Origin != OriginAgent || n.StartedBy != "かな" {
		t.Fatalf("%+v", n)
	}
	n = e.set("%12", map[string]any{"next": "次"})
	if n.Origin != OriginAgent || n.StartedBy != "かな" {
		t.Fatalf("別項目の更新で origin・started_by が消えた: %+v", n)
	}
	n = e.set("%12", map[string]any{"origin": "user"})
	if n.Origin != OriginUser || n.StartedBy != "かな" || n.From != "ゆな（buddy-ai）" {
		t.Fatalf("origin だけが変わるはず: %+v", n)
	}
	n = e.set("%12", map[string]any{"origin": nil, "started_by": nil})
	if n.Origin != "" || n.StartedBy != "" || n.From == "" {
		t.Fatalf("null で消えていない（from は残る）: %+v", n)
	}
	raw, _ := os.ReadFile(e.path)
	if strings.Contains(string(raw), `"origin"`) || strings.Contains(string(raw), `"started_by"`) {
		t.Fatalf("消した項目が JSON に残っている:\n%s", raw)
	}
}

// 新しい項目は from の次に出る（JSON のキー順は宣言順で固定）。既存のメモ（新しい項目なし）の JSON は変わらない。
func TestOriginJSONOrder(t *testing.T) {
	e := newEnv(t)
	n := e.set("%1", map[string]any{"repo": "o/r", "started_by": "かな", "origin": "agent", "from": "ゆな"})
	b, _ := json.Marshal(n)
	want := `{"from":"ゆな","origin":"agent","started_by":"かな","repo":"o/r",`
	if !strings.HasPrefix(string(b), want) {
		t.Fatalf("got %s", b)
	}
	old := e.set("%2", map[string]any{"from": "ゆな"})
	b, _ = json.Marshal(old)
	if strings.Contains(string(b), "origin") || strings.Contains(string(b), "started_by") {
		t.Fatalf("空の新項目が出ている: %s", b)
	}
}

func TestOriginRejectsOtherValues(t *testing.T) {
	e := newEnv(t)
	e.set("%1", map[string]any{"origin": "user"})
	before, _ := os.ReadFile(e.path)
	cases := []struct {
		name  string
		set   map[string]any
		field string
	}{
		{"他の値", map[string]any{"origin": "owner"}, "origin"},
		{"大文字", map[string]any{"origin": "Agent"}, "origin"},
		{"空文字", map[string]any{"origin": ""}, "origin"},
		{"型違い", map[string]any{"origin": true}, "origin"},
		{"started_by 型違い", map[string]any{"started_by": 1}, "started_by"},
		{"started_by 長すぎ", map[string]any{"started_by": strings.Repeat("あ", 2001)}, "started_by"},
		{"正常と同時", map[string]any{"next": "ok", "origin": "bot"}, "origin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.s.Set(context.Background(), "%1", setOf(c.set))
			wantCode(t, err, apperr.InvalidArgs, c.field)
			wantCode(t, Check(setOf(c.set), testDetector), apperr.InvalidArgs, c.field)
		})
	}
	if after, _ := os.ReadFile(e.path); string(before) != string(after) {
		t.Fatal("拒否したのにファイルが変わった")
	}
	if n, _ := e.s.Get("%1"); n.Origin != OriginUser {
		t.Fatalf("origin が変わった: %+v", n)
	}
}

func TestStartedBySecretRejectedWholly(t *testing.T) {
	secret := dummySecret()
	e := newEnv(t)
	e.set("%1", map[string]any{"origin": "user"})
	before, _ := os.ReadFile(e.path)
	_, err := e.s.Set(context.Background(), "%1", setOf(map[string]any{"started_by": secret, "origin": "agent"}))
	wantCode(t, err, apperr.SecretDetected, "started_by")
	if strings.Contains(err.Error(), secret) {
		t.Fatal("エラーに秘密の値が出た")
	}
	if after, _ := os.ReadFile(e.path); string(before) != string(after) {
		t.Fatal("部分書きが起きた")
	}
}

func TestSummariesCarryOriginStartedBy(t *testing.T) {
	e := newEnv(t)
	e.set("%1", map[string]any{"origin": "agent", "started_by": "かな"})
	e.set("%2", map[string]any{"started_by": "かな"})
	e.set("%3", map[string]any{"status": "要約に出ない項目だけ"})
	sums := e.s.Summaries()
	if want := (Summary{Origin: OriginAgent, StartedBy: "かな"}); !reflect.DeepEqual(sums["%1"], want) {
		t.Fatalf("%+v", sums["%1"])
	}
	if _, ok := sums["%2"]; !ok {
		t.Fatal("started_by だけでも要約は空ではない")
	}
	if _, ok := sums["%3"]; ok {
		t.Fatal("要約が空のメモは含めない")
	}
}

// 履歴へ移すときに origin・started_by も残り、from と同じく完全一致で絞れる。from の絞り込みは変わらない。
func TestHistoryFilterOriginStartedBy(t *testing.T) {
	e := newEnv(t)
	seed := []struct {
		id  string
		set map[string]any
	}{
		{"%1", map[string]any{"from": "ゆな", "origin": "agent", "started_by": "かな"}},
		{"%2", map[string]any{"from": "ゆな", "origin": "user"}},
		{"%3", map[string]any{"from": "ゆな（buddy-ai）", "origin": "agent", "started_by": "ゆな"}},
		{"%4", map[string]any{"from": "ゆな"}},
	}
	for _, s := range seed {
		e.set(s.id, s.set)
		e.archive(s.id, "T1", "omp")
	}
	ids := func(f HistoryFilter) []string {
		got, total := e.s.History(f)
		if total != len(got) {
			t.Fatalf("total=%d len=%d", total, len(got))
		}
		out := []string{}
		for _, g := range got {
			out = append(out, g.PaneID)
		}
		return out
	}
	cases := []struct {
		f    HistoryFilter
		want []string
	}{
		{HistoryFilter{Origin: "agent"}, []string{"%3", "%1"}},
		{HistoryFilter{Origin: "user"}, []string{"%2"}},
		{HistoryFilter{StartedBy: "かな"}, []string{"%1"}},
		{HistoryFilter{StartedBy: "か"}, []string{}},
		{HistoryFilter{Origin: "agent", StartedBy: "ゆな"}, []string{"%3"}},
		{HistoryFilter{From: "ゆな", Origin: "agent"}, []string{"%1"}},
		{HistoryFilter{From: "ゆな"}, []string{"%4", "%2", "%1"}},
		{HistoryFilter{Origin: "owner"}, []string{}},
	}
	for _, c := range cases {
		if got := ids(c.f); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v want %v", c.f, got, c.want)
		}
	}
	got, _ := e.s.History(HistoryFilter{StartedBy: "かな"})
	b, _ := json.Marshal(got[0])
	if !strings.Contains(string(b), `"from":"ゆな","origin":"agent","started_by":"かな"`) {
		t.Fatalf("履歴に 2 項目が残っていない: %s", b)
	}
	// 再読み込みしても残る
	again, _ := e.open().History(HistoryFilter{Origin: "agent"})
	if len(again) != 2 || again[1].StartedBy != "かな" {
		t.Fatalf("%+v", again)
	}
}
