package pane

import (
	"reflect"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

func pi(id, session string, window, top, left int) tmux.PaneInfo {
	return tmux.PaneInfo{ID: id, Session: session, Window: window, PaneIndex: len(id), Top: top, Left: left}
}

func TestNamePositions(t *testing.T) {
	base := &config.Config{Sessions: []string{"0"}, DefaultSession: "0"}
	multi := &config.Config{Sessions: []string{"0", "work"}, DefaultSession: "0"}
	tests := []struct {
		name  string
		cfg   *config.Config
		panes []tmux.PaneInfo
		want  map[string]string
	}{
		{"1 ペイン", base, []tmux.PaneInfo{pi("%0", "0", 0, 0, 0)}, map[string]string{"%0": "T1"}},
		{"2x4", base, []tmux.PaneInfo{
			pi("%0", "0", 0, 0, 0), pi("%1", "0", 0, 0, 40), pi("%2", "0", 0, 0, 80), pi("%3", "0", 0, 0, 120),
			pi("%4", "0", 0, 20, 0), pi("%5", "0", 0, 20, 40), pi("%10", "0", 0, 20, 80), pi("%12", "0", 0, 20, 120),
		}, map[string]string{"%0": "T1", "%1": "T2", "%2": "T3", "%3": "T4", "%4": "B1", "%5": "B2", "%10": "B3", "%12": "B4"}},
		{"入力順に依存しない", base, []tmux.PaneInfo{
			pi("%5", "0", 0, 20, 40), pi("%1", "0", 0, 0, 40), pi("%4", "0", 0, 20, 0), pi("%0", "0", 0, 0, 0),
		}, map[string]string{"%0": "T1", "%1": "T2", "%4": "B1", "%5": "B2"}},
		{"3 段は R1..R3", base, []tmux.PaneInfo{
			pi("%0", "0", 0, 0, 0), pi("%1", "0", 0, 10, 0), pi("%2", "0", 0, 10, 50), pi("%3", "0", 0, 20, 0),
		}, map[string]string{"%0": "R11", "%1": "R21", "%2": "R22", "%3": "R31"}},
		{"同じ top で left が飛ぶ", base, []tmux.PaneInfo{
			pi("%0", "0", 0, 0, 0), pi("%1", "0", 0, 0, 77), pi("%2", "0", 0, 0, 200), pi("%3", "0", 0, 30, 5),
		}, map[string]string{"%0": "T1", "%1": "T2", "%2": "T3", "%3": "B1"}},
		{"既定以外のセッションは接頭辞", multi, []tmux.PaneInfo{
			pi("%0", "0", 0, 0, 0), pi("%1", "0", 0, 20, 0), pi("%7", "work", 0, 0, 0), pi("%8", "work", 0, 0, 50),
		}, map[string]string{"%0": "T1", "%1": "B1", "%7": "work:T1", "%8": "work:T2"}},
		{"ウィンドウが複数なら w<index>.（段・列はウィンドウごと）", multi, []tmux.PaneInfo{
			pi("%0", "0", 0, 0, 0), pi("%1", "0", 1, 0, 0), pi("%2", "0", 1, 0, 60), pi("%3", "0", 1, 25, 0),
			pi("%7", "work", 1, 0, 0), pi("%8", "work", 1, 20, 0), pi("%9", "work", 2, 0, 0),
		}, map[string]string{
			"%0": "w0.T1", "%1": "w1.T1", "%2": "w1.T2", "%3": "w1.B1",
			"%7": "work:w1.T1", "%8": "work:w1.B1", "%9": "work:w2.T1",
		}},
		{"ウィンドウ 1 つのセッションには付けない（別セッションが複数でも）", multi, []tmux.PaneInfo{
			pi("%0", "0", 0, 0, 0), pi("%1", "0", 1, 0, 0), pi("%7", "work", 3, 0, 0),
		}, map[string]string{"%0": "w0.T1", "%1": "w1.T1", "%7": "work:T1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NamePositions(tc.panes, tc.cfg); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestNamePositionsFixedNames(t *testing.T) {
	cfg := &config.Config{
		Sessions: []string{"0", "work"}, DefaultSession: "0",
		PaneNames: map[string]string{"0:0.2": "review", "work:0.0": "ci"},
	}
	panes := []tmux.PaneInfo{
		{ID: "%0", Session: "0", Window: 0, PaneIndex: 0, Top: 0, Left: 0},
		{ID: "%1", Session: "0", Window: 0, PaneIndex: 2, Top: 0, Left: 50},
		{ID: "%7", Session: "work", Window: 0, PaneIndex: 0, Top: 0, Left: 0},
	}
	want := map[string]string{"%0": "T1", "%1": "review", "%7": "ci"}
	if got := NamePositions(panes, cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNamePositionsDefaultSessionOmitted(t *testing.T) {
	// default_session が空でセッションが 1 つだけなら、それが既定。
	cfg := &config.Config{Sessions: []string{"solo"}}
	got := NamePositions([]tmux.PaneInfo{{ID: "%0", Session: "solo"}}, cfg)
	if got["%0"] != "T1" {
		t.Fatalf("got %v", got)
	}
}

func TestComputeLayout(t *testing.T) {
	cfg := &config.Config{Sessions: []string{"0", "work"}, DefaultSession: "0"}
	panes := []tmux.PaneInfo{
		pi("%7", "work", 0, 0, 0),
		pi("%0", "0", 0, 0, 0), pi("%1", "0", 0, 0, 40), pi("%2", "0", 0, 0, 80),
		pi("%3", "0", 0, 20, 0), pi("%4", "0", 0, 20, 40),
		pi("%5", "0", 1, 0, 0),
	}
	got := ComputeLayout(panes, cfg)
	if want := (Layout{Rows: 2, Cols: []int{3, 2}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	// 既定セッションにペインが無ければセッション順で最初のセッション。
	got = ComputeLayout([]tmux.PaneInfo{pi("%7", "work", 2, 0, 0), pi("%8", "work", 2, 0, 9)}, cfg)
	if want := (Layout{Rows: 1, Cols: []int{2}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got := ComputeLayout(nil, cfg); got.Rows != 0 || len(got.Cols) != 0 || got.Cols == nil {
		t.Fatalf("empty: %+v", got)
	}
}

func TestFindCaseInsensitive(t *testing.T) {
	s := &Snapshot{Panes: []PaneState{{ID: "%1", Pos: "work:B2"}, {ID: "%2", Pos: "T1"}}}
	for _, arg := range []string{"work:b2", "WORK:B2", "%1"} {
		if p, ok := s.Find(arg); !ok || p.ID != "%1" {
			t.Errorf("Find(%q) = %v %v", arg, p, ok)
		}
	}
	if _, ok := s.Find("%9"); ok {
		t.Error("存在しない ID を見つけた")
	}
}

// NameLocalPositions は接頭辞を付けない以外は NamePositions と同じ（既定以外のセッションも T1 から）。
func TestNameLocalPositionsOmitsSessionPrefix(t *testing.T) {
	cfg := &config.Config{Sessions: []string{"0", "1"}, DefaultSession: "0"}
	panes := []tmux.PaneInfo{
		pi("%0", "0", 0, 0, 0), pi("%1", "0", 0, 20, 0),
		pi("%7", "1", 0, 0, 0), pi("%8", "1", 0, 0, 50), pi("%9", "1", 0, 20, 0),
	}
	want := map[string]string{"%0": "T1", "%1": "B1", "%7": "T1", "%8": "T2", "%9": "B1"}
	if got := NameLocalPositions(panes, cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("local = %v, want %v", got, want)
	}
	// 接頭辞つきの名前は従来どおり。
	if got := NamePositions(panes, cfg); got["%7"] != "1:T1" || got["%9"] != "1:B1" || got["%0"] != "T1" {
		t.Fatalf("global = %v", got)
	}
	// ウィンドウが複数のセッションでは、セッション内でも w<index>. は付く。
	multi := []tmux.PaneInfo{pi("%0", "1", 0, 0, 0), pi("%1", "1", 1, 0, 0)}
	if got := NameLocalPositions(multi, cfg); got["%0"] != "w0.T1" || got["%1"] != "w1.T1" {
		t.Fatalf("multi window = %v", got)
	}
}
