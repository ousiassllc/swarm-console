package notes

import (
	"context"
	"encoding/json"
)

// HistoryFilter は note_history の絞り込み（API 仕様 §5.4）。空は絞り込みなし。
type HistoryFilter struct {
	Repo      string
	From      string
	Origin    string   // 完全一致（from と同じ。値の検証はしない）
	StartedBy string   // 完全一致
	Items     []string // すべて含むもの
	Limit     int      // 既定 20、1〜100（範囲外は呼び出し側が invalid_args にする）
}

// Store は notes.json の読み書き（ER §2、FR-NOTE）。実装は NewStore。
// すべてのエラーは *apperr.Error（invalid_args・secret_detected・internal）。
// 書き込みは一時ファイル→fsync→rename、権限 600、プロセス内排他。
type Store interface {
	// Get は現行メモ。無ければ (nil, false)。
	Get(paneID string) (*Note, bool)
	// Set は渡した項目だけ書き換える。値 null でその項目を消す。
	// 未知キー・型違い・updated_at・log・上限超過は invalid_args、秘密に見える値は secret_detected
	// （details.field に項目名だけ。書き込みは全体拒否）。更新後のメモ全体を返す。
	Set(ctx context.Context, paneID string, set map[string]json.RawMessage) (*Note, error)
	// Log は時刻つき 1 行を足し、直近 50 行を残す。1 行 500 字・改行不可・秘密拒否。ログ行数を返す。メモが無くても作る。
	Log(ctx context.Context, paneID, text string) (int, error)
	// Archive は現行メモを履歴へ移す（履歴 500 件、新しい順）。メモが無ければ false。
	Archive(ctx context.Context, meta ArchiveMeta, closedBy string) (bool, error)
	// History は新しい順。total は絞り込み後の件数（Limit 前）。
	History(f HistoryFilter) (entries []HistoryEntry, total int)
	// Summaries は現行メモの要約（キー: tmux ID）。
	Summaries() map[string]Summary
	// ActiveIDs は現行メモを持つ tmux ID。
	ActiveIDs() []string
	// ReconcileServer は tmux サーバーの起動時刻 start を渡す。保存済みの値があり start と違えば、
	// 現行メモをすべて closed_by "tmux_restart" で履歴へ移す。常に保存値を start にする（P-15）。
	ReconcileServer(ctx context.Context, start string) (archived int, err error)
}
