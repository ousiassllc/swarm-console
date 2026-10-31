package tools

// all は登録順（API 仕様 §1.1）の 17 ツール。見る 9 つのあとに操作 8 つ。
func (e *env) all() []Tool {
	return []Tool{
		e.reportTool(),
		e.panesStatusTool(),
		e.paneTailTool(),
		e.askCardTool(),
		e.reportReadTool(),
		e.hostStatusTool(),
		e.agentAuthStatusTool(),
		e.noteGetTool(),
		e.noteHistoryTool(),
		e.askPickTool(),
		e.sendTextTool(),
		e.sendEscapeTool(),
		e.paneStartTool(),
		e.paneCloseTool(),
		e.dispatchTool(),
		e.noteSetTool(),
		e.noteLogTool(),
	}
}
