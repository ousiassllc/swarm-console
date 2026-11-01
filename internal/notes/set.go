package notes

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func checkCtx(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return apperr.Wrap(apperr.Internal, err)
	}
	return nil
}

func checkPaneID(id string) error {
	if !validPaneID(id) {
		return invalid("pane", reasonBad)
	}
	return nil
}

// Get は現行メモの複製を返す。
func (s *fileStore) Get(paneID string) (*Note, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.data.Panes[paneID]
	if !ok {
		return nil, false
	}
	return cloneNote(n), true
}

// Set は検証（型・上限）と秘密検査をすべて終えてから書く。どれかに当たれば何も書かない。
func (s *fileStore) Set(ctx context.Context, paneID string, set map[string]json.RawMessage) (*Note, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if err := checkPaneID(paneID); err != nil {
		return nil, err
	}
	changes, err := parseChanges(set)
	if err != nil {
		return nil, err
	}
	for _, c := range changes {
		for _, str := range c.strs {
			if str != "" && s.detect(str) {
				return nil, apperr.New(apperr.SecretDetected, "field", c.key)
			}
		}
	}
	var out *Note
	err = s.update(func(d *fileData) error {
		n := cloneNote(d.Panes[paneID])
		for _, c := range changes {
			c.apply(n)
		}
		n.UpdatedAt = s.stamp()
		d.Panes[paneID] = n
		out = cloneNote(n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Log は時刻つき 1 行を足し、直近 50 行を残す。メモが無ければ作る。
func (s *fileStore) Log(ctx context.Context, paneID, text string) (int, error) {
	if err := checkCtx(ctx); err != nil {
		return 0, err
	}
	if err := checkPaneID(paneID); err != nil {
		return 0, err
	}
	if err := checkLogText(text); err != nil {
		return 0, err
	}
	if s.detect(text) {
		return 0, apperr.New(apperr.SecretDetected, "field", "text")
	}
	var size int
	err := s.update(func(d *fileData) error {
		n := cloneNote(d.Panes[paneID])
		at := s.stamp()
		n.Log = append(n.Log, LogEntry{At: at, Text: text})
		if len(n.Log) > maxLogLines {
			n.Log = append([]LogEntry{}, n.Log[len(n.Log)-maxLogLines:]...)
		}
		n.UpdatedAt = at
		d.Panes[paneID] = n
		size = len(n.Log)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return size, nil
}
