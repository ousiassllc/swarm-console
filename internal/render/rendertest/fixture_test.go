package rendertest

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// reportDir・jst・codexExpiry・win・fixedUsage・fixedSnapshot（と report_test.go の mustReport）は、非公開の関数を直接たたく
// 内部パッケージのテスト internal/render/whitebox_test.go にも同じものを置いている（共有できない）。変えたら両方を直す。

var update = flag.Bool("update", false, "ゴールデンファイルを更新する")

const reportDir = "/tmp/swarm-console/reports"

var jst = time.FixedZone("JST", 9*3600)

// codexExpiry は At（22:10:05）の 10 時間 4 分 55 秒後。「あと 10 時間 4 分」（秒は切り捨て）になる。
var codexExpiry = time.Date(2026, 11, 1, 8, 15, 0, 0, jst)

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

// degradedSnapshot は取れない・止めてある・起動直後の値を通す（FR-REP-11）。
func degradedSnapshot() *pane.Snapshot {
	s := calmSnapshot()
	s.Host.CPU = host.CPU{}
	s.Host.Swap = host.Swap{Available: true} // スワップ無し（取得できずとは違う）
	s.Host.Disk = []host.Disk{{Mount: "/", Available: true, FreeGB: 5, TotalGB: 100, UsedPercent: 95}, {Mount: "/mnt/nvme"}}
	s.Usage = fixedUsage()
	s.Usage.Status, s.Usage.Reason = reportvalues.StatusStale, reportvalues.ReasonTimeout
	s.Omp = reportvalues.Omp{Status: reportvalues.OmpUpdateAvailable, Current: "18.14.2", Latest: "18.15.0"}
	s.Update = pane.UpdateInfo{Status: "unknown", Current: "dev", Reason: "dev_build", Apply: pane.ApplyInfo{State: "off"}}
	s.Auth = []host.Auth{{Agent: "omp", Status: host.AuthValid}, {Agent: "claude", Status: host.AuthUnknown}, {Agent: "codex", Status: host.AuthUnknown}}
	return s
}

// pendingSnapshot は起動直後（収集器が最初の取得をする前）。disabledSnapshot は usage_check・update_check を止めたとき。
func pendingSnapshot() *pane.Snapshot {
	s := calmSnapshot()
	s.Usage = reportvalues.Usage{Status: reportvalues.StatusPending, Claude: []reportvalues.Account{}, Cursor: []reportvalues.CursorAccount{}, Codex: []reportvalues.Account{}}
	s.Omp = reportvalues.Omp{Status: reportvalues.OmpPending}
	s.Update = pane.UpdateInfo{Status: "pending", Current: "v0.1.5", Apply: pane.ApplyInfo{State: "idle"}}
	return s
}

func disabledSnapshot() *pane.Snapshot {
	s := pendingSnapshot()
	s.Usage.Status = reportvalues.StatusDisabled
	s.Omp.Status = reportvalues.OmpDisabled
	s.Update.Status = "disabled"
	return s
}

func unavailableSnapshot() *pane.Snapshot {
	s := pendingSnapshot()
	s.Usage.Status, s.Usage.Reason = reportvalues.StatusUnavailable, reportvalues.ReasonNotFound
	s.Omp = reportvalues.Omp{Status: reportvalues.OmpUnknown, Reason: reportvalues.ReasonNotFound, CheckedAt: time.Date(2026, 10, 31, 22, 0, 0, 0, jst)}
	return s
}

// golden は testdata/golden/<name> と got が 1 文字違わず一致することを確かめる。-update で書き直す。
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // テスト用の固定パス
	if err != nil {
		t.Fatalf("ゴールデンを読めない（-update で作る）: %v", err)
	}
	if string(want) != got {
		t.Fatalf("%s が一致しない。\n--- want\n%s\n--- got\n%s", name, want, got)
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
				Note: &notes.Summary{From: "agent-b", Origin: notes.OriginAgent, StartedBy: "かな", Repo: "ousiassllc/funid", Items: []string{"#982"}, Next: "完了したら #983 を入れる"},
			},
			{ID: "%9", Pos: "B1", State: agent.StateShell},
			{ID: "%10", Pos: "B2", Agent: "codex", State: agent.StateUnknown},
			{
				ID: "%11", Pos: "work:B5", Agent: "omp", State: agent.StateWorking, Activity: "a | b\nc",
				Note: &notes.Summary{From: "x|y", Origin: notes.OriginAgent, Repo: "o/r", Items: []string{"#1", "#2"}},
			},
		},
		Warnings:    []pane.Warning{{Code: pane.WarnLayoutChanged, Message: "並びが変わりました: B3 が追加されました"}},
		TmuxSession: true,
	}
}

func calmSnapshot() *pane.Snapshot {
	s := fixedSnapshot()
	s.Panes = []pane.PaneState{
		{ID: "%0", Pos: "T1", Agent: "omp", State: agent.StateIdle},
		{ID: "%1", Pos: "B1", State: agent.StateShell},
	}
	s.Warnings = nil
	s.Auth = []host.Auth{{Agent: "omp", Status: host.AuthValid}, {Agent: "claude", Status: host.AuthValid}, {Agent: "codex", Status: host.AuthValid}}
	return s
}

func fixedDetail() *render.PaneDetail {
	return &render.PaneDetail{
		Note: &notes.Note{
			From: "agent-b", Origin: notes.OriginAgent, StartedBy: "かな", ReportTo: &notes.ReportTo{Kind: "agent", Name: "agent-b", Priority: true},
			Repo: "ousiassllc/funid", Items: []string{"#982"}, Worktree: "/mnt/nvme/ousiassllc/funid-sweep-982",
			Branch: "sweep/issue-sweep-20261031-193532", Instruction: "/tmp/b4-funid-982.txt",
			Approval: &notes.Approval{Text: "残りの1周のスイープをどんどんしていきたい", Via: "agent-b", At: "2026-10-31T19:35:00+09:00"},
			Rules:    []string{"マージしない", "dependabot PR に触らない"}, Status: "CI 待ち", Next: "完了したら #983 を入れる",
			Memo: "1 行目\n2 行目", UpdatedAt: "2026-10-31T21:50:12+09:00",
			Log: []notes.LogEntry{{At: "2026-10-31T19:36:00+09:00", Text: "dispatch した"}},
		},
		Card: &agent.Card{
			Question: "(2) の Go での範囲をどうしますか？", Cursor: 1, AllowsCustom: true,
			Options: []agent.Option{{N: 1, Label: "TS 仕様どおり", Recommended: true}, {N: 2, Label: "ライブラリ関数として拡張"}},
		},
		Tail:   []string{"go test ./...", "ok  	example	0.5s", "``` を含む行"},
		Report: render.ReportFile{Exists: true, UpdatedAt: time.Date(2026, 10, 31, 22, 1, 0, 0, jst)},
	}
}

func testProfiles() config.Profiles {
	return config.Profiles{
		Common: []string{
			"コミットは必ず `git commit -s`（cherry-pick も `-s`）、マージは `git merge --signoff`",
			"マージ・Issue の close・デプロイはしない",
			"最終報告を `{report_path}` にも書く",
		},
		Profiles: map[string]config.Profile{
			"sweep":       {Name: "sweep", Rules: []string{"`--single-pr` で PR は 1 本まで。PR を出して CI が緑になったら止まる"}},
			"fix":         {Name: "fix", Rules: []string{"PR は 1 本まで。バグの再現確認と回帰テストを含める。PR を出して CI が緑になったら止まる"}},
			"investigate": {Name: "investigate", Rules: []string{"調査だけを行い、ファイルの変更・コミット・push・PR 作成はしない。分かったこと・根拠・残りの判断を報告する"}},
		},
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // テスト用の固定パス
	if err != nil {
		t.Fatal(err)
	}
	return b
}
