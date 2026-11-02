package registrytest

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// inSession は p を session のペインにする。
func inSession(p tmux.PaneInfo, session string) tmux.PaneInfo {
	p.Session = session
	return p
}

// threeByTwo はセッション session に 3 列×2 段（T1–T3 / B1–B3）のペインを idBase から登録する。
func threeByTwo(env *panetest.Env, session string, idBase int) []tmux.PaneInfo {
	var ps []tmux.PaneInfo
	for i := range 6 {
		id := "%" + strconv.Itoa(idBase+i)
		p := env.Pane(id, 200+idBase*10+i, (i/3)*20, (i%3)*40, panetest.ScreenIdle("omp"), []string{"omp"})
		ps = append(ps, inSession(p, session))
	}
	return ps
}

func twoSessionEnv() (*panetest.Env, []tmux.PaneInfo, []tmux.PaneInfo) {
	env := panetest.NewEnv()
	env.Cfg.Sessions = []string{"0", "1"}
	s0, s1 := threeByTwo(env, "0", 0), threeByTwo(env, "1", 6)
	env.Tmux.SetPanes(append(append([]tmux.PaneInfo{}, s0...), s1...)...)
	return env, s0, s1
}

// 2 セッション（それぞれ 3 列×2 段）: セッションを絞ると位置は接頭辞なし・layout はそのセッションだけで計算し直す。
func TestForSessionNamesAndLayoutPerSession(t *testing.T) {
	env, _, s1 := twoSessionEnv()
	snap := mustRefresh(t, newTestRegistry(t, env))

	if p := byID(t, snap, s1[0].ID); p.Pos != "1:T1" || p.Local != "T1" || p.ReportPos() != "1:T1" {
		t.Fatalf("全体のスナップショットの位置: %+v", p)
	}
	for _, session := range []string{"0", "1"} {
		got := snap.ForSession(session)
		var pos []string
		for _, p := range got.Panes {
			if p.Session != session {
				t.Fatalf("別セッションのペインが混ざっています: %+v", p)
			}
			pos = append(pos, p.Pos)
		}
		if want := []string{"T1", "T2", "T3", "B1", "B2", "B3"}; !reflect.DeepEqual(pos, want) {
			t.Errorf("セッション %s の位置 = %v, want %v", session, pos, want)
		}
		if want := (pane.Layout{Rows: 2, Cols: []int{3, 3}}); !reflect.DeepEqual(got.Layout, want) {
			t.Errorf("セッション %s の layout = %+v, want %+v", session, got.Layout, want)
		}
	}
	// 最終報告のファイル名はセッションをまたいで衝突しない（絞ったあとも接頭辞つきの位置から決まる）。
	one := snap.ForSession("1")
	if p := one.Panes[0]; p.Pos != "T1" || p.ReportPos() != "1:T1" {
		t.Errorf("ForSession 後: Pos=%q ReportPos=%q", p.Pos, p.ReportPos())
	}
	if p := snap.ForSession("0").Panes[0]; p.ReportPos() != "T1" {
		t.Errorf("既定セッションの ReportPos = %q", p.ReportPos())
	}
	// 元のスナップショットは変えない。
	if p := byID(t, snap, s1[0].ID); p.Pos != "1:T1" {
		t.Errorf("ForSession が元を書き換えました: %+v", p)
	}
}

// 4 列×2 段でも T1–T4 / B1–B4 のまま（FR-POS-2 のアルゴリズムは変えない）。
func TestForSessionFourByTwoKeepsNames(t *testing.T) {
	env := panetest.NewEnv()
	env.Cfg.Sessions = []string{"0", "1"}
	var ps []tmux.PaneInfo
	for i := range 8 {
		p := env.Pane("%"+strconv.Itoa(i), 300+i, (i/4)*20, (i%4)*30, panetest.ScreenIdle("omp"), []string{"omp"})
		ps = append(ps, inSession(p, "1"))
	}
	env.Tmux.SetPanes(ps...)
	got := mustRefresh(t, newTestRegistry(t, env)).ForSession("1")
	var pos []string
	for _, p := range got.Panes {
		pos = append(pos, p.Pos)
	}
	if want := []string{"T1", "T2", "T3", "T4", "B1", "B2", "B3", "B4"}; !reflect.DeepEqual(pos, want) {
		t.Fatalf("pos = %v, want %v", pos, want)
	}
	if want := (pane.Layout{Rows: 2, Cols: []int{4, 4}}); !reflect.DeepEqual(got.Layout, want) {
		t.Fatalf("layout = %+v", got.Layout)
	}
}

// layout_changed は検知もセッションごと。別セッションの変更は、そのセッションを絞った応答にしか出ない。
func TestLayoutChangedWarningIsPerSession(t *testing.T) {
	env, s0, s1 := twoSessionEnv()
	r := newTestRegistry(t, env)
	mustRefresh(t, r)

	extra := inSession(env.Pane("%99", 999, 40, 0, panetest.ScreenIdle("omp"), []string{"omp"}), "1")
	env.Tmux.SetPanes(append(append(append([]tmux.PaneInfo{}, s0...), s1...), extra)...)
	snap := mustRefresh(t, r)

	if w := snap.ForSession("0").Warnings; len(w) != 0 {
		t.Fatalf("セッション 0 に別セッションの警告が出ています: %+v", w)
	}
	w := snap.ForSession("1").Warnings
	if len(w) != 1 || w[0].Code != pane.WarnLayoutChanged || w[0].Session != "1" {
		t.Fatalf("セッション 1 の警告 = %+v", w)
	}
	// 3 段になるので位置が R 系に変わる。メッセージにはセッション内の位置だけが出る（接頭辞 1: は付かない）。
	if !strings.Contains(w[0].Message, "R31 が追加されました") || strings.Contains(w[0].Message, "1:") {
		t.Errorf("message = %q", w[0].Message)
	}
	// TTL が過ぎれば消える。
	env.Clock.Advance(11 * time.Minute)
	if w := mustRefresh(t, r).ForSession("1").Warnings; len(w) != 0 {
		t.Fatalf("TTL 後: %+v", w)
	}
}

// session_missing はセッションごとの警告。絞った応答には、そのセッションの分だけ載る。
func TestSessionMissingIsPerSession(t *testing.T) {
	env := panetest.NewEnv()
	env.Cfg.Sessions = []string{"0", "1", "2"}
	env.Tmux.SetPanes(threeByTwo(env, "0", 0)...)
	snap := mustRefresh(t, newTestRegistry(t, env))

	codes := func(s *pane.Snapshot) []string { return warningCodes(s) }
	if got := codes(snap); !reflect.DeepEqual(got, []string{pane.WarnSessionMissing, pane.WarnSessionMissing}) {
		t.Fatalf("全体の警告 = %v", got)
	}
	if got := snap.ForSession("0"); len(got.Warnings) != 0 || !got.TmuxSession {
		t.Errorf("セッション 0: warnings=%+v tmux=%v", got.Warnings, got.TmuxSession)
	}
	got := snap.ForSession("2")
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0].Message, "2") || strings.Contains(got.Warnings[0].Message, "1") {
		t.Errorf("セッション 2 の警告 = %+v", got.Warnings)
	}
	if got.TmuxSession || len(got.Panes) != 0 || got.Layout.Rows != 0 || got.Layout.Cols == nil || len(got.Layout.Cols) != 0 {
		t.Errorf("存在しないセッション: %+v", got)
	}
}

// pane_names の固定名は、セッションをまたいで一意（Fixed）。Local も固定名になり、接頭辞のない位置と区別できる。
func TestFixedNamePaneIsMarkedFixed(t *testing.T) {
	env, s0, s1 := twoSessionEnv()
	env.Cfg.PaneNames = map[string]string{"1:0." + strconv.Itoa(s1[2].PaneIndex): "review"}
	snap := mustRefresh(t, newTestRegistry(t, env))
	p := byID(t, snap, s1[2].ID)
	if !p.Fixed || p.Pos != "review" || p.Local != "review" || p.ReportPos() != "review" {
		t.Fatalf("固定名のペイン: %+v", p)
	}
	for _, other := range append(append([]tmux.PaneInfo{}, s0...), s1[:2]...) {
		if byID(t, snap, other.ID).Fixed {
			t.Fatalf("固定名でないペインに Fixed が立っている: %s", other.ID)
		}
	}
}
