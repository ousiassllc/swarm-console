package rendertest

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/render"
)

func mustReport(t *testing.T, in render.ReportInput) string {
	t.Helper()
	if in.ReportDir == "" {
		in.ReportDir = reportDir
	}
	out, err := render.Report(in)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	return out
}

func TestReportGolden(t *testing.T) {
	snap := fixedSnapshot()
	cases := []struct {
		name string
		in   render.ReportInput
	}{
		{"report.ja.md", render.ReportInput{Snapshot: snap, Lang: "ja"}},
		{"report.en.md", render.ReportInput{Snapshot: snap, Lang: "en"}},
		{"report.calm.ja.md", render.ReportInput{Snapshot: calmSnapshot(), Lang: "ja"}},
		{"report.calm.en.md", render.ReportInput{Snapshot: calmSnapshot(), Lang: "en"}},
		{"report.pane.ja.md", render.ReportInput{Snapshot: snap, Lang: "ja", PaneID: "b4", Detail: fixedDetail()}},
		{"report.pane.en.md", render.ReportInput{Snapshot: snap, Lang: "en", PaneID: "%12", Detail: fixedDetail()}},
		{"report.pane.nodetail.ja.md", render.ReportInput{Snapshot: snap, Lang: "ja", PaneID: "T1"}},
		{"report.json", render.ReportInput{Snapshot: snap, Format: "json"}},
		{"report.calm.json", render.ReportInput{Snapshot: calmSnapshot(), Format: "json"}},
		{"report.pane.json", render.ReportInput{Snapshot: snap, Format: "json", PaneID: "T2"}},
		{"report.degraded.ja.md", render.ReportInput{Snapshot: degradedSnapshot(), Lang: "ja"}},
		{"report.degraded.en.md", render.ReportInput{Snapshot: degradedSnapshot(), Lang: "en"}},
		{"report.degraded.json", render.ReportInput{Snapshot: degradedSnapshot(), Format: "json"}},
		{"report.pending.ja.md", render.ReportInput{Snapshot: pendingSnapshot(), Lang: "ja"}},
		{"report.pending.json", render.ReportInput{Snapshot: pendingSnapshot(), Format: "json"}},
		{"report.disabled.en.md", render.ReportInput{Snapshot: disabledSnapshot(), Lang: "en"}},
		{"report.disabled.json", render.ReportInput{Snapshot: disabledSnapshot(), Format: "json"}},
		{"report.unavailable.ja.md", render.ReportInput{Snapshot: unavailableSnapshot(), Lang: "ja"}},
		{"report.unavailable.json", render.ReportInput{Snapshot: unavailableSnapshot(), Format: "json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { golden(t, c.name, mustReport(t, c.in)) })
	}
}

func TestPanesStatusJSONGolden(t *testing.T) {
	b, err := render.PanesStatusJSON(fixedSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "panes_status.json", string(b)+"\n")
}

// 同じ入力は 100 回とも同一（FR-REP-7）。
func TestReportDeterministic(t *testing.T) {
	for _, in := range []render.ReportInput{
		{Snapshot: fixedSnapshot(), Lang: "ja"},
		{Snapshot: fixedSnapshot(), Lang: "en", PaneID: "B4", Detail: fixedDetail()},
		{Snapshot: fixedSnapshot(), Format: "json"},
	} {
		in.ReportDir = reportDir
		first := mustReport(t, in)
		for i := 0; i < 100; i++ {
			if got := mustReport(t, in); got != first {
				t.Fatalf("%d 回目で出力が変わった", i)
			}
		}
	}
}

func TestReportDefaultLang(t *testing.T) {
	snap := calmSnapshot()
	if !strings.Contains(mustReport(t, render.ReportInput{Snapshot: snap}), "## ペイン") {
		t.Fatal("既定は ja")
	}
	if !strings.Contains(mustReport(t, render.ReportInput{Snapshot: snap, DefaultLang: "en"}), "## Panes") {
		t.Fatal("DefaultLang が効いていない")
	}
	if !strings.Contains(mustReport(t, render.ReportInput{Snapshot: snap, DefaultLang: "en", Lang: "ja"}), "## ペイン") {
		t.Fatal("Lang が DefaultLang に勝つ")
	}
}

func TestReportRejections(t *testing.T) {
	snap := fixedSnapshot()
	cases := []struct {
		name  string
		in    render.ReportInput
		code  apperr.Code
		field string
	}{
		{"lang", render.ReportInput{Snapshot: snap, Lang: "fr"}, apperr.InvalidArgs, "lang"},
		{"format", render.ReportInput{Snapshot: snap, Format: "html"}, apperr.InvalidArgs, "format"},
		{"pane", render.ReportInput{Snapshot: snap, PaneID: "Z9"}, apperr.PaneNotFound, ""},
		{"pane json", render.ReportInput{Snapshot: snap, PaneID: "Z9", Format: "json"}, apperr.PaneNotFound, ""},
	}
	for _, c := range cases {
		_, err := render.Report(c.in)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != c.code || (c.field != "" && ae.Details["field"] != c.field) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// 呼び出し時刻・クライアント名を含めない: スナップショット以外の入力に時刻は無く、出力の時刻はスナップショットの At。
func TestReportUsesSnapshotTimeOnly(t *testing.T) {
	out := mustReport(t, render.ReportInput{Snapshot: fixedSnapshot()})
	if !strings.Contains(out, "- 時刻: 2026-10-31 22:10:05 (+09:00)\n") {
		t.Fatalf("時刻の形式:\n%s", out)
	}
	if !strings.Contains(out, "load1 98.2 / CPU 42% / メモリ空き 41.3 GB / スワップ 5%") {
		t.Fatal("負荷の丸め")
	}
}

func TestReportAttentionRules(t *testing.T) {
	snap := calmSnapshot()
	out := mustReport(t, render.ReportInput{Snapshot: snap})
	if !strings.Contains(out, "## 要対応\n\n- なし\n") || strings.Contains(out, "## 警告") {
		t.Fatalf("要対応が空なら「なし」、警告が空なら見出しごと出さない:\n%s", out)
	}
	// 報告待ちは idle かつ報告ファイルあり。working で報告ファイルがあっても出ない。
	snap.Panes[0].ReportExists = true
	snap.Panes[1].ReportExists = true // shell
	out = mustReport(t, render.ReportInput{Snapshot: snap})
	if strings.Count(out, "📩") != 1 || !strings.Contains(out, "- 📩 報告待ち: T1 — 最終報告あり（`report_read` で読む）") {
		t.Fatalf("報告待ちの条件:\n%s", out)
	}
	snap.Panes[0].State = "working"
	if strings.Contains(mustReport(t, render.ReportInput{Snapshot: snap}), "📩") {
		t.Fatal("working は報告待ちにならない")
	}
}
