// Package tmux は tmux コマンドの実行と出力の解析を扱う（NFR-SEC-7）。
package tmux

// PaneInfo は list-panes の 1 行。
type PaneInfo struct {
	ID          string // %12
	Session     string
	Window      int
	PaneIndex   int
	Top         int
	Left        int
	Width       int
	Height      int
	PID         int    // ペインのシェルの PID
	CurrentCmd  string // #{pane_current_command}
	Title       string // #{pane_title}
	CurrentPath string // #{pane_current_path}
	Dead        bool
}
