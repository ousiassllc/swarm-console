package pane

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
)

// paneHist はペインごとの画面ハッシュの履歴（changed_at・working の観測開始）。
type paneHist struct {
	hash         string
	changedAt    time.Time
	workingSince time.Time // zero は working でない
}

// seenPane は最後に見えたペインの情報（vanished で履歴へ移すときのメタ）。
type seenPane struct {
	pos   string
	agent string
	title string
}

// layoutWarning は検知済みの並び変更の警告。
type layoutWarning struct {
	msg string
	at  time.Time
}

// posEntry は前回スナップショットの ID と位置（位置順）。
type posEntry struct{ id, pos, session string }

// track はハッシュの履歴を更新し、PaneState の時間まわりの項目を決める。
func (r *SnapshotRegistry) track(id string, o observation, now time.Time) (changedAt time.Time, hash string, state agent.State, elapsed string) {
	h, seen := r.hist[id]
	if !seen {
		h = &paneHist{}
		r.hist[id] = h
	}
	if o.readable {
		switch {
		case !seen && !r.built:
			h.changedAt = r.startedAt // 起動直後は起動時刻（起動後 stalled_after 経つまで stalled にしない）
			h.hash = o.hash
		case !seen || h.hash != o.hash:
			h.changedAt, h.hash = now, o.hash
		}
	} else if !seen {
		h.changedAt = now
	}
	state = o.state
	if state == agent.StateWorking && now.Sub(h.changedAt) >= r.deps.Cfg.StalledAfter {
		state = agent.StateStalled
	}
	if state == agent.StateWorking || state == agent.StateStalled {
		if h.workingSince.IsZero() {
			h.workingSince = now
		}
	} else {
		h.workingSince = time.Time{}
	}
	elapsed = o.elapsed
	if elapsed == "" && !h.workingSince.IsZero() {
		elapsed = FormatDuration(now.Sub(h.workingSince))
	}
	return h.changedAt, h.hash, state, elapsed
}

// layoutDiff は前回との位置の差を日本語の固定文にする。差が無ければ ""。prev・cur は同じセッションのペインで、
// 位置はセッション内の名前（Local）を使う（ほかのセッションの位置を警告に混ぜない）。
func layoutDiff(prev []posEntry, cur []PaneState) string {
	prevPos := make(map[string]string, len(prev))
	for _, e := range prev {
		prevPos[e.id] = e.pos
	}
	curIDs := make(map[string]bool, len(cur))
	var parts []string
	for i := range cur {
		p := &cur[i]
		curIDs[p.ID] = true
		old, ok := prevPos[p.ID]
		switch {
		case !ok:
			parts = append(parts, p.Local+" が追加されました")
		case old != p.Local:
			parts = append(parts, p.Local+" が "+old+"→"+p.Local+" に変わりました")
		}
	}
	for _, e := range prev {
		if !curIDs[e.id] {
			parts = append(parts, e.pos+" が削除されました")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "並びが変わりました: " + strings.Join(parts, ", ")
}

// updateLayout は前回との差をセッションごとに検知し、layout_changed の警告（layout_warning_ttl の間）を返す。
// 比較対象が無い最初の 1 回は警告しない。
func (r *SnapshotRegistry) updateLayout(cur []PaneState, now time.Time) []Warning {
	sessions := r.sessionsOf(cur)
	if r.prevPos != nil {
		for _, s := range sessions {
			var prev []posEntry
			for _, e := range r.prevPos {
				if e.session == s {
					prev = append(prev, e)
				}
			}
			var inSession []PaneState
			for i := range cur {
				if cur[i].Session == s {
					inSession = append(inSession, cur[i])
				}
			}
			if msg := layoutDiff(prev, inSession); msg != "" {
				r.layoutWarn[s] = &layoutWarning{msg: msg, at: now}
			}
		}
	}
	r.prevPos = make([]posEntry, len(cur))
	for i := range cur {
		r.prevPos[i] = posEntry{cur[i].ID, cur[i].Local, cur[i].Session}
	}
	var out []Warning
	for _, s := range sessions {
		w := r.layoutWarn[s]
		if w == nil {
			continue
		}
		if now.Sub(w.at) >= r.deps.Cfg.LayoutWarningTTL {
			delete(r.layoutWarn, s)
			continue
		}
		out = append(out, Warning{Code: WarnLayoutChanged, Message: w.msg, Session: s})
	}
	return out
}

// sessionsOf は今回と前回のスナップショットにあるセッションを、config の sessions の順（それ以外は名前順）で返す。
func (r *SnapshotRegistry) sessionsOf(cur []PaneState) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for i := range cur {
		add(cur[i].Session)
	}
	for _, e := range r.prevPos {
		add(e.session)
	}
	for s := range r.layoutWarn {
		add(s)
	}
	rank := func(s string) int {
		if i := slices.Index(r.deps.Cfg.Sessions, s); i >= 0 {
			return i
		}
		return len(r.deps.Cfg.Sessions)
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a, b)) })
	return out
}

// vanishAfter は「消えた」と判定する連続スナップショット数（P-15）。
const vanishAfter = 2

// trackVanished は見えたペインを覚え、2 回連続で見えなかったペインのメモを履歴へ移す（FR-NOTE-11）。
func (r *SnapshotRegistry) trackVanished(ctx context.Context, cur []PaneState, now time.Time) {
	present := make(map[string]bool, len(cur))
	for i := range cur {
		p := &cur[i]
		present[p.ID] = true
		r.known[p.ID] = seenPane{pos: p.Pos, agent: p.Agent, title: p.Title}
		delete(r.absent, p.ID)
	}
	for id, sp := range r.known {
		if present[id] {
			continue
		}
		r.absent[id]++
		if r.absent[id] < vanishAfter {
			continue
		}
		meta := notes.ArchiveMeta{PaneID: id, Pos: sp.pos, Agent: sp.agent, Title: sp.title, At: now}
		if _, err := r.deps.Notes.Archive(ctx, meta, notes.ClosedByVanished); err != nil {
			r.log.Warn("消えたペインのメモを履歴へ移せませんでした", "pane", id, "err", err)
			continue // 次回やり直す
		}
		delete(r.known, id)
		delete(r.absent, id)
		delete(r.hist, id)
	}
}
