// Package ops は操作系の MCP ツール 10 個（ask_pick・send_text・send_escape・pane_start・pane_close・dispatch・
// note_set・note_log・file_get・dir_list）のハンドラを持つ。/ops だけに出る（API 仕様 §1.1）。
package ops

import "github.com/ousiassllc/swarm-console/internal/tools/toolkit"

// env は操作系のハンドラが共有する依存。
type env struct {
	*toolkit.Env
}

// Tools は操作系のツールを登録順で返す。
func Tools(e *toolkit.Env) []toolkit.Tool {
	h := &env{e}
	return []toolkit.Tool{
		h.askPickTool(),
		h.sendTextTool(),
		h.sendEscapeTool(),
		h.paneStartTool(),
		h.paneCloseTool(),
		h.dispatchTool(),
		h.noteSetTool(),
		h.noteLogTool(),
		h.fileGetTool(),
		h.dirListTool(),
	}
}
