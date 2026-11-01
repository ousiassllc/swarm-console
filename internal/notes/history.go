package notes

import (
	"context"
	"slices"
	"time"
)

// newEntry は現行メモ n を履歴 1 件にする。
func newEntry(n *Note, meta ArchiveMeta, closedAt time.Time, closedBy string) HistoryEntry {
	e := HistoryEntry{
		PaneID:   meta.PaneID,
		Pos:      meta.Pos,
		Title:    meta.Title,
		ClosedAt: closedAt.Format(time.RFC3339),
		ClosedBy: closedBy,
		Note:     *cloneNote(n),
	}
	if meta.Agent != "" {
		a := meta.Agent
		e.Agent = &a
	}
	return e
}

// prependHistory は新しい履歴を先頭に足し、500 件で切った新しいスライスを返す（元は書き換えない）。
func prependHistory(old, add []HistoryEntry) []HistoryEntry {
	total := len(add) + len(old)
	if total > maxHistory {
		total = maxHistory
	}
	out := make([]HistoryEntry, 0, total)
	out = append(out, add...)
	out = append(out, old...)
	return out[:total]
}

// Archive は現行メモを履歴の先頭へ移す。メモが無ければ false。
func (s *fileStore) Archive(ctx context.Context, meta ArchiveMeta, closedBy string) (bool, error) {
	if err := checkCtx(ctx); err != nil {
		return false, err
	}
	closedAt := meta.At
	if closedAt.IsZero() {
		closedAt = s.now()
	}
	moved := false
	err := s.update(func(d *fileData) error {
		n, ok := d.Panes[meta.PaneID]
		if !ok {
			return errNoChange
		}
		delete(d.Panes, meta.PaneID)
		d.History = prependHistory(d.History, []HistoryEntry{newEntry(n, meta, closedAt, closedBy)})
		moved = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return moved, nil
}

// ReconcileServer は tmux サーバーの起動時刻が変わっていたら現行メモをすべて履歴へ移す（P-15）。
func (s *fileStore) ReconcileServer(ctx context.Context, start string) (int, error) {
	if err := checkCtx(ctx); err != nil {
		return 0, err
	}
	archived := 0
	err := s.update(func(d *fileData) error {
		if d.TmuxServerStart == start {
			return errNoChange
		}
		if d.TmuxServerStart != "" {
			now := s.now()
			var add []HistoryEntry
			for _, id := range sortedPaneIDs(d.Panes) {
				add = append(add, newEntry(d.Panes[id], ArchiveMeta{PaneID: id}, now, ClosedByTmuxRestart))
				delete(d.Panes, id)
			}
			d.History = prependHistory(d.History, add)
			archived = len(add)
		}
		d.TmuxServerStart = start
		return nil
	})
	if err != nil {
		return 0, err
	}
	return archived, nil
}

// History は新しい順。total は絞り込み後の件数（Limit 前）。Limit が 0 以下なら 20。
func (s *fileStore) History(f HistoryFilter) ([]HistoryEntry, int) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []HistoryEntry{}
	total := 0
	for _, e := range s.data.History {
		if !matchHistory(e, f) {
			continue
		}
		total++
		if len(out) < limit {
			out = append(out, cloneEntry(e))
		}
	}
	return out, total
}

func matchHistory(e HistoryEntry, f HistoryFilter) bool {
	if f.Repo != "" && e.Repo != f.Repo {
		return false
	}
	if f.From != "" && e.From != f.From {
		return false
	}
	for _, it := range f.Items {
		if !slices.Contains(e.Items, it) {
			return false
		}
	}
	return true
}

// Summaries は現行メモの要約。要約が空のメモは含めない（pane 側ではメモなし扱い）。
func (s *fileStore) Summaries() map[string]Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Summary, len(s.data.Panes))
	for id, n := range s.data.Panes {
		sum := Summary{From: n.From, Repo: n.Repo, Items: cloneStrings(n.Items), Next: n.Next}
		if !sum.IsZero() {
			out[id] = sum
		}
	}
	return out
}

// ActiveIDs は現行メモを持つ tmux ID（数字の昇順）。
func (s *fileStore) ActiveIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedPaneIDs(s.data.Panes)
}
