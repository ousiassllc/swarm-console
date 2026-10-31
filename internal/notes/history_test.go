package notes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/store"
)

func (e *env) archive(id, pos, agent string) bool {
	e.t.Helper()
	ok, err := e.s.Archive(context.Background(), ArchiveMeta{PaneID: id, Pos: pos, Agent: agent, Title: "kana: " + pos, At: e.clk.Now()}, ClosedByPaneClose)
	if err != nil {
		e.t.Fatalf("Archive: %v", err)
	}
	return ok
}

func TestArchiveMovesNote(t *testing.T) {
	e := newEnv(t)
	e.set("%4", map[string]any{"from": "なつみ", "repo": "ousiassllc/funid", "items": []string{"#981"}})
	if !e.archive("%4", "B1", "omp") {
		t.Fatal("メモがあるのに false")
	}
	if _, ok := e.s.Get("%4"); ok {
		t.Fatal("現行メモが残っている")
	}
	if e.archive("%4", "B1", "omp") {
		t.Fatal("メモが無いのに true")
	}
	got, total := e.s.History(HistoryFilter{})
	if total != 1 || got[0].PaneID != "%4" || got[0].Pos != "B1" || got[0].ClosedBy != "pane_close" || got[0].From != "なつみ" {
		t.Fatalf("%+v", got)
	}
}

// API 仕様 §5.4 の例と同じキー順・形で出る（agent は null になり得る）。
func TestHistoryEntryJSONShape(t *testing.T) {
	e := newEnv(t)
	e.set("%4", map[string]any{"from": "なつみ", "repo": "ousiassllc/funid", "items": []string{"#981"}})
	e.archive("%4", "B1", "omp")
	e.set("%5", map[string]any{"from": "ふみ"})
	e.archive("%5", "B2", "")
	got, _ := e.s.History(HistoryFilter{})
	if len(got) != 2 {
		t.Fatal(len(got))
	}
	b, _ := json.Marshal(got[1])
	want := `{"pane_id":"%4","pos":"B1","agent":"omp","title":"kana: B1","closed_at":"2026-10-31T22:10:05+09:00","closed_by":"pane_close","from":"なつみ","repo":"ousiassllc/funid","items":["#981"],"updated_at":"2026-10-31T22:10:05+09:00","log":[]}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(got[0])
	if want := `{"pane_id":"%5","pos":"B2","agent":null,`; string(b)[:len(want)] != want {
		t.Fatalf("agent が null でない: %s", b)
	}
}

func TestHistoryCap500NewestFirst(t *testing.T) {
	// 500 回以上書くので fsync なしの書き込みに差し替える（原子性は別のテストで確かめる）
	e := newEnv(t, WithWriteFile(func(p string, b []byte, m os.FileMode) error {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		return os.WriteFile(p, b, m)
	}))
	for i := 0; i < 505; i++ {
		id := fmt.Sprintf("%%%d", i)
		e.set(id, map[string]any{"from": "x"})
		e.archive(id, "T1", "omp")
	}
	got, total := e.s.History(HistoryFilter{Limit: 100})
	if total != 500 || len(got) != 100 || got[0].PaneID != "%504" || got[99].PaneID != "%405" {
		t.Fatalf("total=%d len=%d first=%s", total, len(got), got[0].PaneID)
	}
	reopened := e.open()
	all, total := reopened.History(HistoryFilter{Limit: 1000})
	if total != 500 || all[499].PaneID != "%5" {
		t.Fatalf("再読込後: total=%d last=%s", total, all[len(all)-1].PaneID)
	}
}

func TestHistorySearch(t *testing.T) {
	e := newEnv(t)
	seed := []struct {
		id, from, repo string
		items          []string
	}{
		{"%1", "ふみ", "o/a", []string{"#1", "#2"}},
		{"%2", "なつみ", "o/a", []string{"#2"}},
		{"%3", "ふみ", "o/b", []string{"#1"}},
	}
	for _, s := range seed {
		e.set(s.id, map[string]any{"from": s.from, "repo": s.repo, "items": s.items})
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
		{HistoryFilter{}, []string{"%3", "%2", "%1"}},
		{HistoryFilter{Repo: "o/a"}, []string{"%2", "%1"}},
		{HistoryFilter{From: "ふみ"}, []string{"%3", "%1"}},
		{HistoryFilter{Items: []string{"#1", "#2"}}, []string{"%1"}},
		{HistoryFilter{Items: []string{"#2"}, From: "なつみ"}, []string{"%2"}},
		{HistoryFilter{Repo: "o"}, []string{}},
		{HistoryFilter{Items: []string{"#9"}}, []string{}},
	}
	for _, c := range cases {
		if got := ids(c.f); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v want %v", c.f, got, c.want)
		}
	}
	got, total := e.s.History(HistoryFilter{Limit: 1})
	if len(got) != 1 || total != 3 {
		t.Fatalf("limit 前の total を返す: len=%d total=%d", len(got), total)
	}
}

func TestHistoryDefaultLimit20(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("%%%d", i)
		e.set(id, map[string]any{"from": "x"})
		e.archive(id, "T1", "")
	}
	got, total := e.s.History(HistoryFilter{})
	if len(got) != 20 || total != 25 {
		t.Fatalf("len=%d total=%d", len(got), total)
	}
}

func TestReconcileServer(t *testing.T) {
	e := newEnv(t)
	writes := 0
	e.s = e.open(WithWriteFile(func(p string, b []byte, m os.FileMode) error {
		writes++
		return store.WriteFileAtomic(p, b, m)
	}))
	ctx := context.Background()
	e.set("%2", map[string]any{"from": "a"})
	e.set("%10", map[string]any{"from": "b"})
	writes = 0
	if n, err := e.s.ReconcileServer(ctx, "1000"); err != nil || n != 0 {
		t.Fatalf("初回は移さない: n=%d err=%v", n, err)
	}
	if n, err := e.s.ReconcileServer(ctx, "1000"); err != nil || n != 0 || writes != 1 {
		t.Fatalf("同じ値は書かない: n=%d writes=%d err=%v", n, writes, err)
	}
	if len(e.s.ActiveIDs()) != 2 {
		t.Fatal("メモが消えた")
	}
	e.clk.Advance(time.Minute)
	n, err := e.s.ReconcileServer(ctx, "2000")
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(e.s.ActiveIDs()) != 0 {
		t.Fatal("現行メモが残っている")
	}
	got, _ := e.s.History(HistoryFilter{})
	if got[0].PaneID != "%2" || got[1].PaneID != "%10" || got[0].ClosedBy != ClosedByTmuxRestart || got[0].ClosedAt != "2026-10-31T22:11:05+09:00" {
		t.Fatalf("%+v", got)
	}
	again := e.open()
	if n, _ := again.ReconcileServer(ctx, "2000"); n != 0 {
		t.Fatal("新しい起動時刻が保存されていない")
	}
}

func TestSummariesAndActiveIDs(t *testing.T) {
	e := newEnv(t)
	e.set("%10", map[string]any{"from": "ふみ", "repo": "o/r", "items": []string{"#1"}, "next": "n", "memo": "出ない"})
	e.set("%2", map[string]any{"status": "要約に出ない項目だけ"})
	e.set("%3", map[string]any{"next": "次"})
	sums := e.s.Summaries()
	if len(sums) != 2 || sums["%10"].From != "ふみ" || sums["%3"].Next != "次" {
		t.Fatalf("%+v", sums)
	}
	if _, ok := sums["%2"]; ok {
		t.Fatal("要約が空のメモは含めない")
	}
	if got := e.s.ActiveIDs(); !reflect.DeepEqual(got, []string{"%2", "%3", "%10"}) {
		t.Fatalf("%v", got)
	}
}
