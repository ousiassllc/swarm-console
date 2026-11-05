package rendertest

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
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

// activity を上限で切ったペインだけ "truncated": true を持つ（API 仕様 §4・§7）。切っていないペインにはキーを出さない。
func TestPaneJSONTruncatedOnlyWhenActivityCut(t *testing.T) {
	snap := fixedSnapshot()
	snap.Panes[1].ActivityTrunc = true // T2（activity あり）だけ切った
	status, err := render.PanesStatusJSON(snap)
	if err != nil {
		t.Fatal(err)
	}
	// キーは note の次（API 仕様 §4。既存のキー順は変えない）。
	if !regexp.MustCompile(`"note":null,"truncated":true`).Match(status) {
		t.Fatalf("truncated が note の次に出ていない: %s", status)
	}
	for name, body := range map[string]string{
		"panes_status": string(status),
		"report":       mustReport(t, render.ReportInput{Snapshot: snap, Format: "json"}),
	} {
		var v struct {
			Panes []map[string]any `json:"panes"`
		}
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range v.Panes {
			got, has := p["truncated"]
			if p["pos"] == "T2" {
				if got != true {
					t.Errorf("%s: T2 に truncated: true が無い: %v", name, p)
				}
			} else if has {
				t.Errorf("%s: 切っていない %v に truncated がある", name, p["pos"])
			}
		}
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

// usage のアカウントの reason は JSON にだけ出る（without_usage・no_email・null）。メール・accountId・orgId は
// 入力に持たないので出力に出ず、markdown は reason の有無で変わらない（P-94）。
func TestReportUsageReason(t *testing.T) {
	snap := fixedSnapshot()
	snap.Usage.Claude = []reportvalues.Account{
		{Name: "acct-a", FiveHour: win(1, "percent", "ok", time.Time{}), SevenDay: win(2, "percent", "ok", time.Time{})},
		{Name: "account-1", Reason: reportvalues.AccountNoEmail, FiveHour: win(3, "percent", "ok", time.Time{})},
		{Name: "acct-z", Reason: reportvalues.AccountWithoutUsage},
	}
	snap.Usage.Cursor = []reportvalues.CursorAccount{{Name: "account-1", Reason: reportvalues.AccountWithoutUsage, Window: "monthly"}}
	var v struct {
		Usage struct {
			Claude []map[string]any `json:"claude"`
			Cursor []map[string]any `json:"cursor"`
			Codex  []map[string]any `json:"codex"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(mustReport(t, render.ReportInput{Snapshot: snap, Format: "json"})), &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		got  map[string]any
		want any
	}{
		{v.Usage.Claude[0], nil},
		{v.Usage.Claude[1], "no_email"},
		{v.Usage.Claude[2], "without_usage"},
		{v.Usage.Cursor[0], "without_usage"},
		{v.Usage.Codex[0], nil},
	} {
		got, has := c.got["reason"]
		if !has || got != c.want {
			t.Errorf("%v: reason = %v (キー有り=%v), want %v", c.got["account"], got, has, c.want)
		}
	}
	// markdown は reason を出さない（reason を外しても同じ）。
	withReason := mustReport(t, render.ReportInput{Snapshot: snap, Lang: "ja"})
	for _, bad := range []string{"without_usage", "no_email"} {
		if strings.Contains(withReason, bad) {
			t.Errorf("markdown に %q が出ている", bad)
		}
	}
	for i := range snap.Usage.Claude {
		snap.Usage.Claude[i].Reason = ""
	}
	snap.Usage.Cursor[0].Reason = ""
	if without := mustReport(t, render.ReportInput{Snapshot: snap, Lang: "ja"}); without != withReason {
		t.Error("markdown が reason で変わっている")
	}
}

// host.mem は MemTotal − MemAvailable（P-93）。MemTotal が 0 以下・MemAvailable が MemTotal より大きい・読めない
// （host がゼロ値）ときは available:false・数値は null。mem_free_gb は変わらず返る。
func TestNewMemJSON(t *testing.T) {
	mk := func(total, free float64) *pane.Snapshot {
		return &pane.Snapshot{Host: host.Status{MemTotalGB: total, MemFreeGB: free}}
	}
	for _, c := range []struct {
		name string
		snap *pane.Snapshot
		want string
	}{
		{"ふつう", mk(128, 41.31), `{"available":true,"total_gb":128,"used_gb":86.7,"used_percent":68}`},
		{"端数", mk(15.6, 3.2), `{"available":true,"total_gb":15.6,"used_gb":12.4,"used_percent":79}`},
		{"空きなし", mk(8, 0), `{"available":true,"total_gb":8,"used_gb":8,"used_percent":100}`},
		{"四捨五入で上がる", mk(8, 3.96), `{"available":true,"total_gb":8,"used_gb":4,"used_percent":51}`},
		{"四捨五入で下がる", mk(8, 4.08), `{"available":true,"total_gb":8,"used_gb":3.9,"used_percent":49}`},
		{"全部空き", mk(8, 8), `{"available":true,"total_gb":8,"used_gb":0,"used_percent":0}`},
		{"MemTotal が 0", mk(0, 0), `{"available":false,"total_gb":null,"used_gb":null,"used_percent":null}`},
		{"MemTotal が 0 で空きだけある", mk(0, 4), `{"available":false,"total_gb":null,"used_gb":null,"used_percent":null}`},
		{"空きが全体より大きい", mk(4, 8), `{"available":false,"total_gb":null,"used_gb":null,"used_percent":null}`},
		{"読めない", &pane.Snapshot{}, `{"available":false,"total_gb":null,"used_gb":null,"used_percent":null}`},
	} {
		b, err := json.Marshal(render.NewMemJSON(c.snap))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Errorf("%s: mem = %s, want %s", c.name, b, c.want)
		}
	}
}

// report の host に mem が入り、mem_free_gb は残る（キー順: load1, mem_free_gb, mem, cpu）。
func TestReportHostMem(t *testing.T) {
	body := mustReport(t, render.ReportInput{Snapshot: fixedSnapshot(), Format: "json"})
	want := `"host":{"load1":98.2,"mem_free_gb":41.3,"mem":{"available":true,"total_gb":128,"used_gb":86.7,"used_percent":68},"cpu":`
	if !strings.Contains(body, want) {
		t.Errorf("host.mem がない:\n%s", body)
	}
}
