// Package render は report・dispatch の出力を組み立てる純関数群（FR-REP・FR-DIS）。
// 同じ入力からは、呼び出し時刻・クライアントによらず 1 文字も違わない出力を返す（FR-REP-7）。
package render

import (
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// 出力形式と言語。
const (
	FormatMarkdown = "markdown"
	FormatJSON     = "json"
	LangJA         = "ja"
	LangEN         = "en"
)

// tailLines は詳細版に出す画面の行数（FR-REPORT §5.3）。
const tailLines = 15

// ReportFile は最終報告ファイルの有無と更新時刻。
type ReportFile struct {
	Exists    bool
	UpdatedAt time.Time
}

// PaneDetail は pane 指定の詳しい版に足す情報。tools 層が集めて渡す。
type PaneDetail struct {
	Note   *notes.Note // メモ全項目。無ければ nil
	Card   *agent.Card // 質問カード。asking でなければ nil
	Tail   []string    // 画面の最後の行（飾りなし）。多ければ最後の 15 行だけ使う
	Report ReportFile
}

// ReportInput は Report の入力。
type ReportInput struct {
	Snapshot *pane.Snapshot
	Lang     string // "" なら DefaultLang
	Format   string // "" なら markdown
	// PaneID は位置または tmux ID。空なら全体。解決できなければ apperr.PaneNotFound。
	PaneID string
	// Detail は PaneID 指定の詳しい版に出す内容。markdown のときだけ使う。
	Detail *PaneDetail
	// Templates は読み込み済みのテンプレート。nil なら TemplatesDir から読む（空なら組み込みの既定）。
	Templates    *Templates
	TemplatesDir string
	// DefaultLang は Lang が空のときの言語（config の default_lang）。空なら ja。
	DefaultLang string
	// ReportDir は最終報告ファイルの置き場（<ReportDir>/<位置>.md）。
	ReportDir string
}
