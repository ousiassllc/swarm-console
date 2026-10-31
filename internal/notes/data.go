package notes

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func emptyData() fileData {
	return fileData{Version: fileVersion, Panes: map[string]*Note{}, History: []HistoryEntry{}}
}

func marshalData(d *fileData) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// parseData は notes.json を読み、形を整える。読めない・版が違うときはエラー（退避の対象）。
func parseData(raw []byte) (fileData, error) {
	var d fileData
	if err := json.Unmarshal(raw, &d); err != nil {
		return fileData{}, err
	}
	if d.Version != fileVersion {
		return fileData{}, fmt.Errorf("未対応の version %d", d.Version)
	}
	if d.Panes == nil {
		d.Panes = map[string]*Note{}
	}
	for id, n := range d.Panes {
		if n == nil {
			return fileData{}, errors.New("メモが null です")
		}
		normalizeNote(n)
		d.Panes[id] = n
	}
	if d.History == nil {
		d.History = []HistoryEntry{}
	}
	if len(d.History) > maxHistory {
		d.History = d.History[:maxHistory]
	}
	for i := range d.History {
		normalizeNote(&d.History[i].Note)
	}
	return d, nil
}

// normalizeNote は log を常に非 nil にし、50 行へそろえる。
func normalizeNote(n *Note) {
	if n.Log == nil {
		n.Log = []LogEntry{}
	}
	if len(n.Log) > maxLogLines {
		n.Log = n.Log[len(n.Log)-maxLogLines:]
	}
}

// cloneNote は深いコピー。nil なら空のメモ（log は []）を返す。
func cloneNote(n *Note) *Note {
	if n == nil {
		return &Note{Log: []LogEntry{}}
	}
	c := *n
	c.Items = cloneStrings(n.Items)
	c.Rules = cloneStrings(n.Rules)
	if n.ReportTo != nil {
		r := *n.ReportTo
		c.ReportTo = &r
	}
	if n.Approval != nil {
		a := *n.Approval
		c.Approval = &a
	}
	c.Log = append([]LogEntry{}, n.Log...)
	return &c
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string{}, s...)
}

func cloneEntry(e HistoryEntry) HistoryEntry {
	n := cloneNote(&e.Note)
	e.Note = *n
	if e.Agent != nil {
		a := *e.Agent
		e.Agent = &a
	}
	return e
}

// validPaneID は "%" ＋ 数字か。
func validPaneID(id string) bool {
	if len(id) < 2 || id[0] != '%' {
		return false
	}
	_, err := strconv.ParseUint(id[1:], 10, 64)
	return err == nil && !strings.ContainsAny(id[1:], "+-")
}

// sortedPaneIDs は tmux ID を数字の昇順で返す。
func sortedPaneIDs(m map[string]*Note) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, aerr := strconv.ParseUint(strings.TrimPrefix(ids[i], "%"), 10, 64)
		b, berr := strconv.ParseUint(strings.TrimPrefix(ids[j], "%"), 10, 64)
		if aerr != nil || berr != nil {
			return ids[i] < ids[j]
		}
		return a < b
	})
	return ids
}
