package pane

import "github.com/ousiassllc/swarm-console/internal/tmux"

// ForSession は session のペインだけを残したスナップショットのコピーを返す（セッションを絞った応答の元）。
//   - Panes は session のペイン。Pos はセッション内の位置（Local）になる（最終報告のファイル名は ReportPos が引き続き接頭辞つき）。
//   - Layout は session のペインから計算し直す。
//   - Warnings は session のものと、セッションに属さないもの（tmux_unavailable）だけ。
//   - TmuxSession は session にペインがあるか。
//
// 負荷・使用状況・更新・ログイン期限は絞らない。元のスナップショットは変更しない。
func (s *Snapshot) ForSession(session string) *Snapshot {
	out := *s
	out.Panes = make([]PaneState, 0, len(s.Panes))
	var infos []tmux.PaneInfo
	for i := range s.Panes {
		p := s.Panes[i]
		if p.Session != session {
			continue
		}
		p.GlobalPos = p.ReportPos()
		p.Pos = p.LocalPos()
		out.Panes = append(out.Panes, p)
		infos = append(infos, tmux.PaneInfo{Session: p.Session, Window: p.Window, PaneIndex: p.PaneIndex, Top: p.Top, Left: p.Left})
	}
	out.Layout = ComputeLayout(infos, nil)
	out.Warnings = nil
	for _, w := range s.Warnings {
		if w.Session == "" || w.Session == session {
			out.Warnings = append(out.Warnings, w)
		}
	}
	out.TmuxSession = len(out.Panes) > 0
	return &out
}
