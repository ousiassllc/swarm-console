package pane

import (
	"sort"
	"strconv"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// defaultSession は位置の接頭辞を省略するセッション。default_session が空でセッションが 1 つだけならそれ（FR-POS-1）。
func defaultSession(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.DefaultSession == "" && len(cfg.Sessions) == 1 {
		return cfg.Sessions[0]
	}
	return cfg.DefaultSession
}

// SortPanes は位置順（セッション順 → ウィンドウ → 段 → 列）に並べたコピーを返す。
// セッション順は cfg.Sessions の順で、未知のセッションは最後に名前順。
func SortPanes(panes []tmux.PaneInfo, cfg *config.Config) []tmux.PaneInfo {
	out := append([]tmux.PaneInfo(nil), panes...)
	order := map[string]int{}
	if cfg != nil {
		for i, s := range cfg.Sessions {
			order[s] = i
		}
	}
	rank := func(s string) int {
		if i, ok := order[s]; ok {
			return i
		}
		return len(order)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case rank(a.Session) != rank(b.Session):
			return rank(a.Session) < rank(b.Session)
		case a.Session != b.Session:
			return a.Session < b.Session
		case a.Window != b.Window:
			return a.Window < b.Window
		case a.Top != b.Top:
			return a.Top < b.Top
		case a.Left != b.Left:
			return a.Left < b.Left
		}
		return a.PaneIndex < b.PaneIndex
	})
	return out
}

type windowKey struct {
	session string
	window  int
}

// rowsOf は 1 ウィンドウのペインを段ごとに分ける。段は Top の異なる値の昇順、段内は Left → PaneIndex 順。
func rowsOf(panes []tmux.PaneInfo) [][]tmux.PaneInfo {
	sorted := append([]tmux.PaneInfo(nil), panes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Top != b.Top {
			return a.Top < b.Top
		}
		if a.Left != b.Left {
			return a.Left < b.Left
		}
		return a.PaneIndex < b.PaneIndex
	})
	var rows [][]tmux.PaneInfo
	for i, p := range sorted {
		if i == 0 || p.Top != sorted[i-1].Top {
			rows = append(rows, nil)
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], p)
	}
	return rows
}

// rowName は段の名前。1 段は T、2 段は T・B、3 段以上は R1・R2…（FR-POS-2）。
func rowName(i, n int) string {
	switch {
	case n >= 3:
		return "R" + strconv.Itoa(i+1)
	case i == 0:
		return "T"
	}
	return "B"
}

// NamePositions はペインの座標から位置の名前を付ける（FR-POS-1〜5・P-07）。キーは tmux ID。
// 段・列の判定はウィンドウごと。固定名（pane_names）があれば自動名より優先する。
func NamePositions(panes []tmux.PaneInfo, cfg *config.Config) map[string]string {
	return namePositions(panes, cfg, false)
}

// NameLocalPositions は NamePositions と同じ規則で、セッションの接頭辞を付けない名前（セッション内の位置）を付ける。
// セッションを絞った応答（panes_status など）とペイン引数の解決に使う。
func NameLocalPositions(panes []tmux.PaneInfo, cfg *config.Config) map[string]string {
	return namePositions(panes, cfg, true)
}

func namePositions(panes []tmux.PaneInfo, cfg *config.Config, local bool) map[string]string {
	windows := map[windowKey][]tmux.PaneInfo{}
	perSession := map[string]map[int]bool{}
	for _, p := range panes {
		k := windowKey{p.Session, p.Window}
		windows[k] = append(windows[k], p)
		if perSession[p.Session] == nil {
			perSession[p.Session] = map[int]bool{}
		}
		perSession[p.Session][p.Window] = true
	}
	def := defaultSession(cfg)
	names := make(map[string]string, len(panes))
	for k, ps := range windows {
		prefix := ""
		if k.session != def && !local {
			prefix = k.session + ":"
		}
		if len(perSession[k.session]) > 1 {
			prefix += "w" + strconv.Itoa(k.window) + "."
		}
		rows := rowsOf(ps)
		for r, row := range rows {
			for c, p := range row {
				names[p.ID] = prefix + rowName(r, len(rows)) + strconv.Itoa(c+1)
				if n, ok := fixedName(p, cfg); ok {
					names[p.ID] = n
				}
			}
		}
	}
	return names
}

// fixedName は pane_names の固定名（キーは <セッション>:<ウィンドウ>.<ペイン番号>）。あればその名前と true。
// 固定名はセッションをまたいで一意（接頭辞のない位置のように、セッション内でだけ通じる名前ではない）。
func fixedName(p tmux.PaneInfo, cfg *config.Config) (string, bool) {
	if cfg == nil {
		return "", false
	}
	key := p.Session + ":" + strconv.Itoa(p.Window) + "." + strconv.Itoa(p.PaneIndex)
	n, ok := cfg.PaneNames[key]
	return n, ok && n != ""
}

// ComputeLayout は panes_status の layout を返す。複数セッションのときは、既定セッション
// （ペインが無ければセッション順で最初にペインのあるセッション）の最初のウィンドウの段数と各段の列数。
func ComputeLayout(panes []tmux.PaneInfo, cfg *config.Config) Layout {
	sorted := SortPanes(panes, cfg)
	if len(sorted) == 0 {
		return Layout{Cols: []int{}}
	}
	session := sorted[0].Session
	def := defaultSession(cfg)
	for _, p := range sorted {
		if def != "" && p.Session == def {
			session = def
			break
		}
	}
	window, found := 0, false
	var target []tmux.PaneInfo
	for _, p := range sorted {
		if p.Session != session {
			continue
		}
		if !found {
			window, found = p.Window, true
		}
		if p.Window == window {
			target = append(target, p)
		}
	}
	rows := rowsOf(target)
	cols := make([]int, len(rows))
	for i, r := range rows {
		cols[i] = len(r)
	}
	return Layout{Rows: len(rows), Cols: cols}
}
