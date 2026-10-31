package pane

import "strings"

func findPane(s *Snapshot, arg string) (*PaneState, bool) {
	for i := range s.Panes {
		p := &s.Panes[i]
		if p.ID == arg || strings.EqualFold(p.Pos, arg) {
			return p, true
		}
	}
	return nil, false
}
