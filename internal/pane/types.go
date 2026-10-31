// Package pane はペイン台帳・位置解決・スナップショット・サンプラー・ロック・操作の実行を担うドメインの中心。
package pane

import (
	"context"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// 警告コード（API 仕様 §5.1 panes_status）。
const (
	WarnLayoutChanged   = "layout_changed"
	WarnTmuxUnavailable = "tmux_unavailable"
	WarnSessionMissing  = "session_missing"
	WarnPaneUnreadable  = "pane_unreadable"
)

// Warning はスナップショットの警告。
type Warning struct {
	Code    string
	Message string
}

// Layout は panes_status の layout。Cols は段ごとの列数（上の段から）。
type Layout struct {
	Rows int
	Cols []int
}

// PaneState は 1 ペインのスナップショット上の状態（ER §4）。作ったあと変更しない。
type PaneState struct {
	ID        string // %12
	Pos       string // T1 / work:B2 / w1.T2（固定名があればそれ）
	Session   string
	Window    int
	PaneIndex int
	Top       int
	Left      int

	Agent         string // omp | claude | codex。"" は null
	State         agent.State
	Title         string
	Cwd           string
	Elapsed       string // "42m"。"" は null
	Activity      string // 120 字で切り済み。"" は null
	ActivityTrunc bool   // 切ったか
	ChangedAgo    string // "20s"・"1h05m"。"" は null
	ScreenHash    string
	ChangedAt     time.Time
	ReportExists  bool
	Note          *notes.Summary // メモがなければ nil
}

// Snapshot は不変のスナップショット。At が report の時刻になる。
type Snapshot struct {
	At       time.Time
	Host     host.Status
	Layout   Layout
	Panes    []PaneState // 位置順（セッション順 → ウィンドウ → 段 → 列）
	Warnings []Warning
	// TmuxSession は対象セッションのいずれかが存在するか（host_status.tmux_session）。
	TmuxSession bool
}

// Find は ID または位置（大文字小文字を区別しない）でペインを探す。
func (s *Snapshot) Find(arg string) (*PaneState, bool) { return findPane(s, arg) }

// Ref は解決済みのペイン参照。
type Ref struct {
	ID  string
	Pos string
}

// Tail は pane_tail の結果。
type Tail struct {
	Text      string
	Truncated bool
}

// Registry は読み取り側（機能要件 §4.1）。スナップショットが snapshot_ttl 以内ならキャッシュ、古ければ singleflight で取り直す。
type Registry interface {
	Current(ctx context.Context) (*Snapshot, error)
	// Refresh は強制再取得。
	Refresh(ctx context.Context) (*Snapshot, error)
	// Resolve は位置（大文字小文字不問）または tmux ID を解決する。解決できなければ apperr.PaneNotFound。
	Resolve(ctx context.Context, arg string) (Ref, error)
	// Tail は最後の lines 行。raw=false なら Clean 済み（agent が unknown なら raw 相当）。
	// 1 行 500 字・全体 20,000 字で切り、切ったら Truncated。
	Tail(ctx context.Context, ref Ref, lines int, raw bool) (Tail, error)
	// Card は質問カード。asking でなければ (nil, nil)。agent が unknown なら apperr.UnsupportedAgent。
	Card(ctx context.Context, ref Ref) (*agent.Card, error)
}

// PickResult は ask_pick の結果。
type PickResult struct {
	OK         bool
	State      agent.State
	Picked     *agent.Option // 選んだ選択肢（text のときは nil）
	CardClosed bool
}

// SendResult は send_text の結果。
type SendResult struct {
	OK       bool
	State    agent.State
	Accepted bool
	Chars    int
}

// EscapeResult は send_escape の結果。
type EscapeResult struct {
	OK          bool
	State       agent.State
	Interrupted bool
}

// StartResult は pane_start の結果。
type StartResult struct {
	OK    bool
	State agent.State
	Agent string
	Cwd   string
	Title string // "agent: ..." 付きの最終タイトル
}

// CloseResult は pane_close の結果。
type CloseResult struct {
	OK           bool
	State        agent.State
	Steps        []string
	NoteArchived bool
}

// DispatchArgs は組み立て・検証済みの dispatch 入力。指示文は tools 層が render で作る。
type DispatchArgs struct {
	Agent string
	Cwd   string // shell のとき必須。検証済み（safety.CheckCWD 済み）
	Title string // shell のとき必須
	Text  string // 組み立て後の全文（8000 字以内・制御文字検証済み）
}

// DispatchResult は dispatch の結果。メモの保存は呼び出し側（tools）が行う。
type DispatchResult struct {
	OK       bool
	State    agent.State
	Accepted bool
	Started  bool
}

// Ops は操作側（機能要件 §4.2）。どの操作も同じペインについて排他（lock_timeout 超過で apperr.BusyTimeout）。
// 事前条件を満たさなければ何も送らず *apperr.Error を返す。送信後に期待どおりでなければ OK=false と現在の State を返す。
type Ops interface {
	PickAsk(ctx context.Context, ref Ref, n int, text string) (PickResult, error)
	SendText(ctx context.Context, ref Ref, text string) (SendResult, error)
	Escape(ctx context.Context, ref Ref) (EscapeResult, error)
	Start(ctx context.Context, ref Ref, agentName, cwd, title string) (StartResult, error)
	Close(ctx context.Context, ref Ref) (CloseResult, error)
	Dispatch(ctx context.Context, ref Ref, a DispatchArgs) (DispatchResult, error)
}

// NoteStore は pane が使うメモ側の口（notes.Store が実装する）。
type NoteStore interface {
	// Summaries は現行メモの要約（キー: tmux ID）。
	Summaries() map[string]notes.Summary
	// ActiveIDs は現行メモを持つ tmux ID。
	ActiveIDs() []string
	// Archive は現行メモを履歴へ移す。メモが無ければ false。
	Archive(ctx context.Context, meta notes.ArchiveMeta, closedBy string) (bool, error)
	// Log は note_log と同じ検証で 1 行足す（send_escape の自動記録）。メモが無くても作る。
	Log(ctx context.Context, paneID, text string) error
	TmuxServerStart() string
	SetTmuxServerStart(ctx context.Context, v string) error
}

// Tmux は pane が使う tmux 側の口（tmux.Client が実装する）。
type Tmux interface {
	// ListPanes は sessions の全ペインを返す。存在しないセッションはエラーにせず結果に含めない。
	ListPanes(ctx context.Context, sessions []string) ([]tmux.PaneInfo, error)
	// Capture は画面の最後から lines 行を capture-pane -p（ANSI なし、折り返し結合なし）で返す。
	Capture(ctx context.Context, id string, lines int) (string, error)
	// SendKeys は tmux のキー名（Up・Down・Enter・Escape・数字など）を send-keys する。
	SendKeys(ctx context.Context, id string, keys ...string) error
	// SendLiteral は send-keys -l で文字列をそのまま送る（制御文字は呼び出し側で除外済み）。
	SendLiteral(ctx context.Context, id, text string) error
	// Paste は load-buffer -（標準入力・一意なバッファ名）＋ paste-buffer -p -d で bracketed paste する。
	Paste(ctx context.Context, id, text string) error
	// SetTitle は select-pane -T。
	SetTitle(ctx context.Context, id, title string) error
	// ServerStart は #{start_time}（tmux サーバーの起動時刻）。サーバーが無ければ ""。
	ServerStart(ctx context.Context) (string, error)
	// Version は tmux -V の出力。
	Version(ctx context.Context) (string, error)
}
