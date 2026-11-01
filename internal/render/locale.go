package render

import (
	"github.com/ousiassllc/swarm-console/internal/agent"
)

// locale は言語ごとの語句（FR-REP §5.2・§5.3）。見出し・表頭はテンプレート側に置く。
type locale struct {
	states      map[agent.State]string
	asking      string // 要対応の行頭
	stalled     string
	reportWait  string
	noChange    string // "変化なし 20m"
	reportReady string
	yes, no     string
	exists      string // 最終報告: あり（…）
	missing     string
	updated     string
	fields      [fieldCount]string // メモの項目名（noteFields の順）
}

var locales = map[string]locale{
	LangJA: {
		states: map[agent.State]string{
			agent.StateWorking: "🟢 作業中",
			agent.StateAsking:  "🟡 質問待ち",
			agent.StateIdle:    "⚪ 入力待ち",
			agent.StateShell:   "⬜ 空き",
			agent.StateStalled: "🔴 止まってそう",
			agent.StateUnknown: "❔ 不明",
		},
		asking: "🟡 質問待ち", stalled: "🔴 止まってそう", reportWait: "📩 報告待ち",
		noChange: "変化なし ", reportReady: "最終報告あり（`report_read` で読む）",
		yes: "可", no: "不可", exists: "あり", missing: "なし", updated: "更新 ",
		fields: [fieldCount]string{
			"依頼元", "報告先", "リポジトリ", "Issue・PR", "作業フォルダ", "ブランチ", "指示ファイル",
			"OK の原文", "守ること", "いまの状況", "次にやること", "再開 ID", "メモ", "更新",
		},
	},
	LangEN: {
		states: map[agent.State]string{
			agent.StateWorking: "🟢 Working",
			agent.StateAsking:  "🟡 Asking",
			agent.StateIdle:    "⚪ Idle",
			agent.StateShell:   "⬜ Free",
			agent.StateStalled: "🔴 Stalled",
			agent.StateUnknown: "❔ Unknown",
		},
		asking: "🟡 Asking", stalled: "🔴 Stalled", reportWait: "📩 Awaiting report",
		noChange: "no change ", reportReady: "final report available (read with `report_read`)",
		yes: "allowed", no: "not allowed", exists: "exists", missing: "none", updated: "updated ",
		fields: [fieldCount]string{
			"From", "Report to", "Repo", "Issues / PRs", "Worktree", "Branch", "Instruction file",
			"Approval", "Rules", "Status", "Next", "Resume ID", "Memo", "Updated",
		},
	},
}

// stateWord は状態の言葉。未知の状態は値そのまま。
func (l locale) stateWord(s agent.State) string {
	if w, ok := l.states[s]; ok {
		return w
	}
	return string(s)
}
