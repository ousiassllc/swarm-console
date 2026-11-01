package rendertest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// pane 指定の JSON：panes と、ペインの要対応は絞られるが、ログインの要対応・使用状況・更新・auth は絞られない（FR-REP-16）。
func TestReportJSONPaneKeepsLoginItems(t *testing.T) {
	out := mustReport(t, render.ReportInput{Snapshot: fixedSnapshot(), Format: "json", PaneID: "T2"})
	var got struct {
		Panes     []struct{ Pos string }
		Attention []struct{ Kind, Pane, Detail string }
		Usage     struct{ Claude []struct{ Account string } }
		Auth      struct {
			Agents []struct{ Agent, Status string }
		}
		Update struct{ Status string }
		Omp    struct{ Status string }
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, a := range got.Attention {
		kinds = append(kinds, a.Kind+"/"+a.Pane+"/"+a.Detail)
	}
	want := "asking/T2/(2) の Go での範囲をどうしますか？ auth_expired//claude auth_expiring//codex"
	if len(got.Panes) != 1 || got.Panes[0].Pos != "T2" || strings.Join(kinds, " ") != want {
		t.Fatalf("panes=%+v attention=%v", got.Panes, kinds)
	}
	if len(got.Usage.Claude) != 3 || len(got.Auth.Agents) != 3 || got.Update.Status != "up_to_date" || got.Omp.Status != "up_to_date" {
		t.Fatalf("値が絞られた: %s", out)
	}
	// 詳しい版（markdown）も、見出し・要対応・使用状況は全体の版と同じ。
	md := mustReport(t, render.ReportInput{Snapshot: fixedSnapshot(), PaneID: "B4", Detail: fixedDetail()})
	for _, w := range []string{"- ログイン: omp 不明", "- 🔑 ログイン期限切れ: claude", "## 使用状況", "- 更新: omp 18.14.2（最新）"} {
		if !strings.Contains(md, w) {
			t.Errorf("詳しい版に %q が無い", w)
		}
	}
}

// FR-REP-7：同じスナップショットは、収集器が結果を更新しても、何回描画しても 1 文字も変わらない。
// 描画は入力のスナップショットを書き換えない。
func TestValuesRenderIsPureAndStable(t *testing.T) {
	snap := degradedSnapshot()
	before := fmt.Sprintf("%+v", *snap)
	var first []string
	for round := range 3 {
		var outs []string
		for _, in := range []render.ReportInput{
			{Snapshot: snap, Lang: "ja"}, {Snapshot: snap, Lang: "en"}, {Snapshot: snap, Format: "json"},
			{Snapshot: snap, Lang: "en", PaneID: "T1", Detail: fixedDetail()},
		} {
			outs = append(outs, mustReport(t, in))
		}
		if round == 0 {
			first = outs
			// 収集器が（別のスナップショットの）値を更新しても、このスナップショットの出力は変わらない。
			other := *snap
			other.Usage = reportvalues.Usage{Status: reportvalues.StatusOK}
			other.Omp = reportvalues.Omp{Status: reportvalues.OmpUpToDate, Current: "99.0.0", Latest: "99.0.0"}
			_ = mustReport(t, render.ReportInput{Snapshot: &other, Lang: "ja"})
			continue
		}
		for i := range outs {
			if outs[i] != first[i] {
				t.Fatalf("round %d の出力 %d が変わった", round, i)
			}
		}
	}
	if after := fmt.Sprintf("%+v", *snap); after != before {
		t.Fatal("描画がスナップショットを書き換えた")
	}
}

// FR-REP-18：メール・accountId・orgId・トークン・omp のコマンド出力は、どの出力にも出ない。
// 収集器を通した録った出力（メールを含む）を、全形式・全言語で描画して確かめる。
func TestNoSecretsInOutputs(t *testing.T) {
	u, err := reportvalues.ParseUsage(mustRead(t, "../../reportvalues/testdata/usage_ok.json"))
	if err != nil {
		t.Fatal(err)
	}
	snap := fixedSnapshot()
	snap.Usage = u
	snap.Omp = reportvalues.Omp{Status: reportvalues.OmpUpdateAvailable, Current: "18.14.2", Latest: "18.15.0"}
	var all strings.Builder
	for _, lang := range []string{render.LangJA, render.LangEN} {
		all.WriteString(mustReport(t, render.ReportInput{Snapshot: snap, Lang: lang}))
	}
	all.WriteString(mustReport(t, render.ReportInput{Snapshot: snap, Format: "json"}))
	for _, bad := range []string{"@", "user", ".example", "accountId", "orgId", "email", "Current version", "New version available", "eyJ"} {
		if strings.Contains(all.String(), bad) {
			t.Errorf("出力に %q が出ている", bad)
		}
	}
	if !strings.Contains(all.String(), "acct-a") {
		t.Error("アカウント名は出る")
	}
}
