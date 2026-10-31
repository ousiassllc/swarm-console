package render

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
)

var update = flag.Bool("update", false, "ゴールデンファイルを更新する")

const reportDir = "/tmp/swarm-console/reports"

var jst = time.FixedZone("JST", 9*3600)

// golden は testdata/golden/<name> と got が 1 文字違わず一致することを確かめる。-update で書き直す。
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
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
		At:     time.Date(2026, 10, 31, 22, 10, 5, 0, jst),
		Host:   host.Status{Load: [3]float64{98.24, 95.1, 90.4}, CPUs: 32, MemFreeGB: 41.31},
		Layout: pane.Layout{Rows: 2, Cols: []int{4, 4}},
		Panes: []pane.PaneState{
			{ID: "%0", Pos: "T1", Agent: "omp", State: agent.StateIdle, ReportExists: true, Cwd: "/mnt/nvme/ousiassllc/funid", Title: "kana: funid #981"},
			{ID: "%1", Pos: "T2", Agent: "claude", State: agent.StateAsking, Activity: "(2) の Go での範囲をどうしますか？", Elapsed: "3m", ChangedAgo: "5s"},
			{ID: "%8", Pos: "B3", Agent: "omp", State: agent.StateStalled, ChangedAgo: "20m", Activity: "Waiting"},
			{
				ID: "%12", Pos: "B4", Agent: "omp", State: agent.StateWorking, Title: "kana: funid #982",
				Cwd: "/mnt/nvme/ousiassllc/funid-sweep-982", Elapsed: "42m", Activity: "Running go test ./...", ChangedAgo: "20s",
				Note: &notes.Summary{From: "ふみ", Repo: "ousiassllc/funid", Items: []string{"#982"}, Next: "完了したら #983 を入れる"},
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

func calmSnapshot() *pane.Snapshot {
	s := fixedSnapshot()
	s.Panes = []pane.PaneState{
		{ID: "%0", Pos: "T1", Agent: "omp", State: agent.StateIdle},
		{ID: "%1", Pos: "B1", State: agent.StateShell},
	}
	s.Warnings = nil
	return s
}

func fixedDetail() *PaneDetail {
	return &PaneDetail{
		Note: &notes.Note{
			From: "ふみ", ReportTo: &notes.ReportTo{Kind: "agent", Name: "ふみ", Priority: true},
			Repo: "ousiassllc/funid", Items: []string{"#982"}, Worktree: "/mnt/nvme/ousiassllc/funid-sweep-982",
			Branch: "sweep/issue-sweep-20261031-193532", Instruction: "/tmp/b4-funid-982.txt",
			Approval: &notes.Approval{Text: "残りの1周のスイープをどんどんしていきたい", Via: "ふみ", At: "2026-10-31T19:35:00+09:00"},
			Rules:    []string{"マージしない", "dependabot PR に触らない"}, Status: "CI 待ち", Next: "完了したら #983 を入れる",
			Memo: "1 行目\n2 行目", UpdatedAt: "2026-10-31T21:50:12+09:00",
			Log: []notes.LogEntry{{At: "2026-10-31T19:36:00+09:00", Text: "dispatch した"}},
		},
		Card: &agent.Card{
			Question: "(2) の Go での範囲をどうしますか？", Cursor: 1, AllowsCustom: true,
			Options: []agent.Option{{N: 1, Label: "TS 仕様どおり", Recommended: true}, {N: 2, Label: "ライブラリ関数として拡張"}},
		},
		Tail:   []string{"go test ./...", "ok  	example	0.5s", "``` を含む行"},
		Report: ReportFile{Exists: true, UpdatedAt: time.Date(2026, 10, 31, 22, 1, 0, 0, jst)},
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
