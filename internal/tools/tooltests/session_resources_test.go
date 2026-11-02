package tooltests_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

func listPanes(t *testing.T, f *toolstest.Fixture, c auth.Client) []string {
	t.Helper()
	panes, err := f.Tools.Panes(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range panes {
		out = append(out, p.Pos+"="+p.ID)
	}
	return out
}

// resources/list は許可されたセッションのペインだけ。許可が 1 つならセッション内の位置、2 つ以上なら接頭辞つきの位置。
func TestResourcePanesFollowACL(t *testing.T) {
	f := twoSessions(t)
	if got, want := listPanes(t, f, kana), []string{"T1=%1", "T2=%2", "B1=%3", "B2=%4", "B3=%5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("kana = %v", got)
	}
	if got, want := listPanes(t, f, natsumi), []string{"T1=%11", "T2=%12", "T3=%13", "B1=%14", "B2=%15", "B3=%16"}; !reflect.DeepEqual(got, want) {
		t.Errorf("natsumi = %v（セッション内の位置）", got)
	}
	got := listPanes(t, f, boss)
	if len(got) != 11 || got[0] != "T1=%1" || got[5] != "1:T1=%11" {
		t.Errorf("boss（許可 2 つ）= %v（セッションをまたいで一意な名前）", got)
	}
	if !f.Tools.ReportReadable(kana) || !f.Tools.ReportReadable(natsumi) || f.Tools.ReportReadable(boss) {
		t.Error("swarm://report を読めるのは、許可がちょうど 1 つのトークンだけ")
	}
}

func TestResourceReadFollowsACL(t *testing.T) {
	f := twoSessions(t)
	ctx := context.Background()
	// 許可内: セッションの中の位置・tmux ID。
	if md, err := f.Tools.ReportMarkdown(ctx, natsumi, "T1"); err != nil || !strings.Contains(md, "%11") {
		t.Errorf("natsumi の T1 = %v %s", err, md)
	}
	if md, err := f.Tools.ReportMarkdown(ctx, kana, ""); err != nil || strings.Contains(md, "1:T") {
		t.Errorf("kana の全体 = %v %s", err, md)
	}
	if text, err := f.Tools.NoteJSON(ctx, natsumi, "%12"); err != nil || !strings.Contains(text, `"pane":"T2"`) {
		t.Errorf("natsumi のメモ = %v %s", err, text)
	}
	// 許可外: ID・全体の名前いずれでも session_forbidden。存在しない位置は pane_not_found。
	for _, pane := range []string{"%11", "1:T1"} {
		if _, err := f.Tools.ReportMarkdown(ctx, kana, pane); apperr.As(err).Code != apperr.SessionForbidden {
			t.Errorf("kana ReportMarkdown(%s): %v", pane, err)
		}
		if _, err := f.Tools.NoteJSON(ctx, kana, pane); apperr.As(err).Code != apperr.SessionForbidden {
			t.Errorf("kana NoteJSON(%s): %v", pane, err)
		}
	}
	if _, err := f.Tools.ReportMarkdown(ctx, kana, "ZZ"); apperr.As(err).Code != apperr.PaneNotFound {
		t.Errorf("存在しない位置: %v", err)
	}
	// 許可が複数: swarm://report は session を決められないので session_required。ペインは全体の名前か ID。
	if _, err := f.Tools.ReportMarkdown(ctx, boss, ""); apperr.As(err).Code != apperr.SessionRequired {
		t.Errorf("boss の全体: %v", err)
	}
	if md, err := f.Tools.ReportMarkdown(ctx, boss, "1:T1"); err != nil || !strings.Contains(md, "%11") {
		t.Errorf("boss の 1:T1 = %v", err)
	}
	if _, err := f.Tools.NoteJSON(ctx, boss, "T3"); apperr.As(err).Code != apperr.PaneNotFound {
		t.Errorf("boss はセッション内の位置（T3）を使えない: %v", err)
	}
}
