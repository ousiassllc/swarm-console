// Package view は読み取り系の MCP ツール 10 個（report・sessions_list・panes_status・pane_tail・ask_card・
// report_read・host_status・agent_auth_status・note_get・note_history）のハンドラと、
// resources/list・resources/read が使う同じ本文の組み立てを持つ。/report と /ops の両方に出る（API 仕様 §1.1）。
package view

import "github.com/ousiassllc/swarm-console/internal/tools/toolkit"

// env は読み取り系のハンドラが共有する依存。
type env struct {
	*toolkit.Env
}

// Tools は読み取り系のツールを登録順で返す。
func Tools(e *toolkit.Env) []toolkit.Tool {
	h := &env{e}
	return []toolkit.Tool{
		h.reportTool(),
		h.sessionsListTool(),
		h.panesStatusTool(),
		h.paneTailTool(),
		h.askCardTool(),
		h.reportReadTool(),
		h.hostStatusTool(),
		h.agentAuthStatusTool(),
		h.noteGetTool(),
		h.noteHistoryTool(),
	}
}
