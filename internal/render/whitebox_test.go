package render

import (
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// 非公開の words・attentionItems を直接たたくテスト。公開 API だけで書けるテストは rendertest にある。
// 次の helper は rendertest の複製（外部パッケージのテストとは共有できない）：fixture_test.go の reportDir・jst・codexExpiry・
// win・fixedUsage・fixedSnapshot と、report_test.go の mustReport。片方を変えたら、もう片方も同じに直す。
const reportDir = "/tmp/swarm-console/reports"

var jst = time.FixedZone("JST", 9*3600)

var codexExpiry = time.Date(2026, 11, 1, 8, 15, 0, 0, jst)

func mustReport(t *testing.T, in ReportInput) string {
	t.Helper()
	if in.ReportDir == "" {
		in.ReportDir = reportDir
	}
	out, err := Report(in)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	return out
}

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

func win(used float64, unit, status string, resets time.Time) reportvalues.Window {
	return reportvalues.Window{Available: true, Used: used, Unit: unit, Status: status, ResetsAt: resets}
}

// fixedUsage は機能要件 §5.3 の例に、上限到達・USD・枠なしを足した使用状況。
func fixedUsage() reportvalues.Usage {
	return reportvalues.Usage{
		Status: reportvalues.StatusOK, FetchedAt: time.Date(2026, 10, 31, 22, 9, 30, 0, jst),
		Claude: []reportvalues.Account{
			{Name: "acct-a", SevenDay: win(48, "percent", "ok", time.Date(2026, 11, 1, 7, 0, 0, 0, jst))},
			{Name: "acct-b", FiveHour: win(0, "percent", "ok", time.Time{}), SevenDay: win(7, "percent", "ok", time.Date(2026, 11, 6, 10, 0, 0, 0, jst))},
			// 収集器は resets_at を UTC の瞬間で持つ。描画で At のオフセット（JST）に直す（15:00 UTC → 翌 00:00）。
			{Name: "acct-c", FiveHour: win(100, "percent", "exhausted", time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)), SevenDay: win(61.5, "percent", "ok", time.Date(2026, 11, 3, 0, 0, 0, 0, jst))},
		},
		Cursor: []reportvalues.CursorAccount{
			{Name: "acct-a", Window: "monthly", Models: win(0, "percent", "ok", time.Date(2026, 12, 1, 9, 0, 0, 0, jst))},
			{Name: "acct-b", Window: "weekly", Models: win(0, "usd", "ok", time.Date(2026, 11, 2, 9, 0, 0, 0, jst))},
		},
		Codex: []reportvalues.Account{
			{Name: "acct-a", FiveHour: win(0, "percent", "ok", time.Date(2026, 11, 2, 3, 9, 0, 0, jst)), SevenDay: win(2, "percent", "ok", time.Date(2026, 11, 4, 9, 0, 0, 0, jst))},
		},
	}
}

// fixedSnapshot は API 仕様 §5.1・機能要件 §5.3 の例に近い固定のスナップショット。
func fixedSnapshot() *pane.Snapshot {
	return &pane.Snapshot{
		At: time.Date(2026, 10, 31, 22, 10, 5, 0, jst),
		Host: host.Status{
			Load: [3]float64{98.24, 95.1, 90.4}, CPUs: 32, MemFreeGB: 41.31,
			CPU:  host.CPU{Available: true, UsedPercent: 42},
			Swap: host.Swap{Available: true, TotalGB: 8, UsedGB: 0.4, UsedPercent: 5},
			Disk: []host.Disk{
				{Mount: "/", Available: true, FreeGB: 120.2, TotalGB: 450, UsedPercent: 73},
				{Mount: "/mnt/nvme", Available: true, FreeGB: 830.4, TotalGB: 3700, UsedPercent: 78},
			},
		},
		Usage: fixedUsage(), Omp: reportvalues.Omp{Status: reportvalues.OmpUpToDate, Current: "18.14.2", Latest: "18.14.2", CheckedAt: time.Date(2026, 10, 31, 9, 0, 0, 0, jst)},
		Update: pane.UpdateInfo{
			Status: "up_to_date", Current: "v0.1.5", Latest: "v0.1.5", CheckedAt: time.Date(2026, 10, 31, 12, 0, 0, 0, jst),
			Apply: pane.ApplyInfo{State: "idle"},
		},
		Auth: []host.Auth{
			{Agent: "omp", Status: host.AuthUnknown},
			{Agent: "claude", Status: host.AuthExpired},
			{Agent: "codex", Status: host.AuthExpiringSoon, ExpiresAt: &codexExpiry},
		},
		Layout: pane.Layout{Rows: 2, Cols: []int{4, 4}},
		Panes: []pane.PaneState{
			{ID: "%0", Pos: "T1", Agent: "omp", State: agent.StateIdle, ReportExists: true, Cwd: "/mnt/nvme/ousiassllc/funid", Title: "agent: funid #981"},
			{ID: "%1", Pos: "T2", Agent: "claude", State: agent.StateAsking, Activity: "(2) の Go での範囲をどうしますか？", Elapsed: "3m", ChangedAgo: "5s"},
			{ID: "%8", Pos: "B3", Agent: "omp", State: agent.StateStalled, ChangedAgo: "20m", Activity: "Waiting"},
			{
				ID: "%12", Pos: "B4", Agent: "omp", State: agent.StateWorking, Title: "agent: funid #982",
				Cwd: "/mnt/nvme/ousiassllc/funid-sweep-982", Elapsed: "42m", Activity: "Running go test ./...", ChangedAgo: "20s",
				Note: &notes.Summary{From: "agent-b", Repo: "ousiassllc/funid", Items: []string{"#982"}, Next: "完了したら #983 を入れる"},
			},
			{ID: "%9", Pos: "B1", State: agent.StateShell},
			{ID: "%10", Pos: "B2", Agent: "codex", State: agent.StateUnknown},
			{
				ID: "%11", Pos: "work:B5", Agent: "omp", State: agent.StateWorking, Activity: "a | b\nc",
				Note: &notes.Summary{From: "x|y", Repo: "o/r", Items: []string{"#1", "#2"}},
			},
		},
		Warnings:    []pane.Warning{{Code: pane.WarnLayoutChanged, Message: "並びが変わりました: B3 が追加されました"}},
		TmuxSession: true,
	}
}
