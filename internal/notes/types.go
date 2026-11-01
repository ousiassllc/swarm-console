// Package notes はペインごとのメモと履歴（notes.json）を扱う（ER §2）。
package notes

import "time"

// Summary は panes_status・report に出すメモの要約（FR-NOTE-3）。
type Summary struct {
	From  string   `json:"from,omitempty"`
	Repo  string   `json:"repo,omitempty"`
	Items []string `json:"items,omitempty"`
	Next  string   `json:"next,omitempty"`
}

// IsZero は要約が空かを返す。
func (s Summary) IsZero() bool {
	return s.From == "" && s.Repo == "" && len(s.Items) == 0 && s.Next == ""
}

// ArchiveMeta は履歴へ移すときに添える、閉じたときのペイン情報（ER §2.4）。
type ArchiveMeta struct {
	PaneID string
	Pos    string
	Agent  string // "" は null
	Title  string
	At     time.Time
}

// closed_by の値。
const (
	ClosedByPaneClose   = "pane_close"
	ClosedByVanished    = "vanished"
	ClosedByTmuxRestart = "tmux_restart"
)
