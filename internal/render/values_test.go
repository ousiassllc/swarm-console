package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// 「あと N 時間」は expires_at と At の差（分未満は切り捨て）から描画で計算する（ja・en。機能要件 §5.3・§5.4）。
func TestRemaining(t *testing.T) {
	tests := []struct {
		d        time.Duration
		ja, en   string
		describe string
	}{
		{-time.Minute, "あと 0 分", "in 0 min", "過ぎている"},
		{59*time.Second + 999*time.Millisecond, "あと 0 分", "in 0 min", "分未満は切り捨て"},
		{5 * time.Minute, "あと 5 分", "in 5 min", "1 時間未満"},
		{59*time.Minute + 59*time.Second, "あと 59 分", "in 59 min", "1 時間の直前"},
		{time.Hour, "あと 1 時間", "in 1 h", "ちょうど 1 時間（M が 0）"},
		{10*time.Hour + 5*time.Minute, "あと 10 時間 5 分", "in 10 h 5 min", "24 時間未満"},
		{24*time.Hour - time.Minute, "あと 23 時間 59 分", "in 23 h 59 min", "24 時間の直前"},
		{24 * time.Hour, "あと 1 日", "in 1 d", "ちょうど 1 日（M が 0）"},
		{2*24*time.Hour + 3*time.Hour + 59*time.Minute, "あと 2 日 3 時間", "in 2 d 3 h", "24 時間以上は分を出さない"},
	}
	for _, tt := range tests {
		t.Run(tt.describe, func(t *testing.T) {
			if got := jaWords.remaining(tt.d); got != tt.ja {
				t.Errorf("ja = %q, want %q", got, tt.ja)
			}
			if got := enWords.remaining(tt.d); got != tt.en {
				t.Errorf("en = %q, want %q", got, tt.en)
			}
		})
	}
}

// ログインの要対応：期限切れ → 期限間近の順で、unknown・valid は入れない。ペインに属さないので pane は ""。
func TestAttentionAuthItems(t *testing.T) {
	exp := time.Date(2026, 11, 1, 8, 0, 0, 0, jst)
	auth := []host.Auth{
		{Agent: "omp", Status: host.AuthExpiringSoon, ExpiresAt: &exp},
		{Agent: "claude", Status: host.AuthUnknown},
		{Agent: "codex", Status: host.AuthExpired},
	}
	got := attentionItems(nil, reportDir, auth)
	if len(got) != 2 || got[0].Kind != kindAuthExpired || got[0].Detail != "codex" || got[1].Kind != kindAuthExpiring || got[1].Detail != "omp" || got[0].Pane != "" {
		t.Fatalf("%+v", got)
	}
	// ペインの項目のあとに並ぶ。
	s := fixedSnapshot()
	got = attentionItems(s.Panes, reportDir, auth)
	if n := len(got); n != 5 || got[2].Kind != kindReportWaiting || got[3].Kind != kindAuthExpired {
		t.Fatalf("並び: %+v", got)
	}
	// 期限が分からない期限間近は「あと」を出さない。
	s.Auth = []host.Auth{{Agent: "codex", Status: host.AuthExpiringSoon}}
	if out := mustReport(t, ReportInput{Snapshot: s}); !strings.Contains(out, "- ⏳ ログイン期限間近: codex\n") {
		t.Errorf("期限なしの期限間近:\n%s", out)
	}
}

// pane 指定の JSON：panes と、ペインの要対応は絞られるが、ログインの要対応・使用状況・更新・auth は絞られない（FR-REP-16）。
func TestReportJSONPaneKeepsLoginItems(t *testing.T) {
	out := mustReport(t, ReportInput{Snapshot: fixedSnapshot(), Format: "json", PaneID: "T2"})
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
	md := mustReport(t, ReportInput{Snapshot: fixedSnapshot(), PaneID: "B4", Detail: fixedDetail()})
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
		for _, in := range []ReportInput{
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
			_ = mustReport(t, ReportInput{Snapshot: &other, Lang: "ja"})
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
	u, err := reportvalues.ParseUsage(mustRead(t, "../reportvalues/testdata/usage_ok.json"))
	if err != nil {
		t.Fatal(err)
	}
	snap := fixedSnapshot()
	snap.Usage = u
	snap.Omp = reportvalues.Omp{Status: reportvalues.OmpUpdateAvailable, Current: "18.14.2", Latest: "18.15.0"}
	var all strings.Builder
	for _, lang := range []string{LangJA, LangEN} {
		all.WriteString(mustReport(t, ReportInput{Snapshot: snap, Lang: lang}))
	}
	all.WriteString(mustReport(t, ReportInput{Snapshot: snap, Format: "json"}))
	for _, bad := range []string{"@", "user", ".example", "accountId", "orgId", "email", "Current version", "New version available", "eyJ"} {
		if strings.Contains(all.String(), bad) {
			t.Errorf("出力に %q が出ている", bad)
		}
	}
	if !strings.Contains(all.String(), "acct-a") {
		t.Error("アカウント名は出る")
	}
}
