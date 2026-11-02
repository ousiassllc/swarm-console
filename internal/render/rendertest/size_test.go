package rendertest

import (
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// NFR-PERF-3（docs/requirements/non-functional.md）の応答サイズの上限（バイト）。
const (
	maxPanesStatusBytes = 8 * 1024 // panes_status 8 ペイン
	maxReportBytes      = 6 * 1024 // report（markdown）
)

// eightPaneSnapshot は 8 ペイン・5 アカウント分の使用状況・メモ・活動・要対応が入った、現実的な固定のスナップショット。
func eightPaneSnapshot() *pane.Snapshot {
	s := fixedSnapshot()
	s.Usage = fixedUsage()
	s.Usage.Claude = []reportvalues.Account{
		{Name: "acct-a", FiveHour: win(35, "percent", "ok", time.Date(2026, 10, 31, 23, 0, 0, 0, jst)), SevenDay: win(48, "percent", "ok", time.Date(2026, 11, 1, 7, 0, 0, 0, jst))},
		{Name: "acct-b", SevenDay: win(7, "percent", "ok", time.Date(2026, 11, 6, 10, 0, 0, 0, jst))},
		{Name: "acct-c", FiveHour: win(100, "percent", "exhausted", time.Date(2026, 10, 31, 15, 0, 0, 0, time.UTC)), SevenDay: win(61.5, "percent", "ok", time.Date(2026, 11, 3, 0, 0, 0, 0, jst))},
		{Name: "acct-d", FiveHour: win(12, "percent", "ok", time.Date(2026, 11, 1, 1, 30, 0, 0, jst)), SevenDay: win(80, "percent", "ok", time.Date(2026, 11, 2, 9, 0, 0, 0, jst))},
		{Name: "acct-e", FiveHour: win(0, "usd", "ok", time.Time{}), SevenDay: win(3.4, "usd", "ok", time.Date(2026, 11, 5, 9, 0, 0, 0, jst))},
	}
	s.Layout = pane.Layout{Rows: 2, Cols: []int{4, 4}}
	s.Panes = []pane.PaneState{
		{
			ID: "%0", Pos: "T1", Agent: "omp", State: agent.StateWorking, Title: "agent: funid #981",
			Cwd: "/mnt/nvme/ousiassllc/funid-sweep-981", Elapsed: "1h20m", Activity: "Running go test -race ./internal/...", ChangedAgo: "3s",
			Note: &notes.Summary{From: "agent-a", Repo: "ousiassllc/funid", Items: []string{"#981", "#984"}, Next: "CI が緑になったら #985 を入れる"},
		},
		{
			ID: "%1", Pos: "T2", Agent: "claude", State: agent.StateAsking, Title: "agent: swarm-console #51",
			Cwd: "/mnt/nvme/ousiassllc/swarm-console-w-51", Elapsed: "3m", Activity: "(2) の Go での範囲をどうしますか？", ChangedAgo: "5s",
			Note: &notes.Summary{From: "agent-b", Repo: "ousiassllc/swarm-console", Items: []string{"#51"}, Next: "回答を受けて実装を再開する"},
		},
		{ID: "%2", Pos: "T3", Agent: "omp", State: agent.StateIdle, ReportExists: true, Title: "agent: dotfiles #12", Cwd: "/home/ousiass/dotfiles", ChangedAgo: "12m"},
		{
			ID: "%3", Pos: "T4", Agent: "codex", State: agent.StateStalled, Title: "agent: funid #990",
			Cwd: "/mnt/nvme/ousiassllc/funid-sweep-990", Elapsed: "2h5m", Activity: "Waiting for approval", ChangedAgo: "20m",
			Note: &notes.Summary{From: "agent-c", Repo: "ousiassllc/funid", Items: []string{"#990"}},
		},
		{
			ID: "%4", Pos: "B1", Agent: "omp", State: agent.StateWorking, Title: "agent: funid #982",
			Cwd: "/mnt/nvme/ousiassllc/funid-sweep-982", Elapsed: "42m", Activity: "Editing internal/render/report.go", ChangedAgo: "20s",
			Note: &notes.Summary{From: "agent-b", Repo: "ousiassllc/funid", Items: []string{"#982", "#983"}, Next: "完了したら #986 を入れる"},
		},
		{ID: "%5", Pos: "B2", State: agent.StateShell, Cwd: "/mnt/nvme/ousiassllc/swarm-console"},
		{
			ID: "%6", Pos: "B3", Agent: "claude", State: agent.StateWorking, Title: "agent: swarm-console #52",
			Cwd: "/mnt/nvme/ousiassllc/swarm-console-w-52", Elapsed: "9m", Activity: "Reading docs/requirements/non-functional.md", ChangedAgo: "1s",
			Note: &notes.Summary{From: "agent-d", Repo: "ousiassllc/swarm-console", Items: []string{"#52"}},
		},
		{ID: "%7", Pos: "B4", Agent: "codex", State: agent.StateUnknown},
	}
	return s
}

func TestResponseSizeLimits(t *testing.T) {
	snap := eightPaneSnapshot()
	if len(snap.Panes) != 8 {
		t.Fatalf("panes = %d, want 8", len(snap.Panes))
	}

	b, err := render.PanesStatusJSON(snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxPanesStatusBytes {
		t.Errorf("panes_status（8 ペイン）= %d バイト, 上限 %d", len(b), maxPanesStatusBytes)
	}
	t.Logf("panes_status（8 ペイン）= %d バイト", len(b))

	for _, lang := range []string{render.LangJA, render.LangEN} {
		out := mustReport(t, render.ReportInput{Snapshot: snap, Lang: lang})
		if len(out) > maxReportBytes {
			t.Errorf("report markdown（%s, 8 ペイン）= %d バイト, 上限 %d", lang, len(out), maxReportBytes)
		}
		t.Logf("report markdown（%s, 8 ペイン）= %d バイト", lang, len(out))
	}
}
