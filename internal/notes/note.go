package notes

// origin の値（ER §2.2）。空は「どちらでもない」。
const (
	OriginUser  = "user"  // オーナーの指示
	OriginAgent = "agent" // エージェントが自分の判断で始めた
)

// ReportTo は報告先（ER §2.3）。
type ReportTo struct {
	Kind      string `json:"kind"` // agent | email
	Name      string `json:"name,omitempty"`
	Priority  bool   `json:"priority,omitempty"`
	To        string `json:"to,omitempty"`
	Subject   string `json:"subject,omitempty"`
	ReplyToID string `json:"reply_to_id,omitempty"`
}

// Approval は OK の原文（ER §2.3）。
type Approval struct {
	Text string `json:"text"`
	Via  string `json:"via,omitempty"`
	At   string `json:"at,omitempty"`
}

// LogEntry はメモのログ 1 行。
type LogEntry struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// Note は現行メモ（ER §2.2）。JSON のキー順はこの宣言順で固定（API 仕様 §4）。
// Log は常に出す（空でも []）。
type Note struct {
	From        string     `json:"from,omitempty"`
	Origin      string     `json:"origin,omitempty"`
	StartedBy   string     `json:"started_by,omitempty"`
	ReportTo    *ReportTo  `json:"report_to,omitempty"`
	Repo        string     `json:"repo,omitempty"`
	Items       []string   `json:"items,omitempty"`
	Worktree    string     `json:"worktree,omitempty"`
	Branch      string     `json:"branch,omitempty"`
	Instruction string     `json:"instruction,omitempty"`
	Approval    *Approval  `json:"approval,omitempty"`
	Rules       []string   `json:"rules,omitempty"`
	Status      string     `json:"status,omitempty"`
	Next        string     `json:"next,omitempty"`
	ResumeID    string     `json:"resume_id,omitempty"`
	Memo        string     `json:"memo,omitempty"`
	UpdatedAt   string     `json:"updated_at,omitempty"`
	Log         []LogEntry `json:"log"`
}

// HistoryEntry は履歴 1 件（ER §2.4）。Note の全項目に閉じたときの情報を足したもの。
type HistoryEntry struct {
	PaneID   string  `json:"pane_id"`
	Pos      string  `json:"pos"`
	Agent    *string `json:"agent"` // nil は null
	Title    string  `json:"title"`
	ClosedAt string  `json:"closed_at"`
	ClosedBy string  `json:"closed_by"`
	Note
}
