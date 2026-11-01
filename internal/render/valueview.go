package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// このファイルは、スナップショットに入った値を markdown の言葉にする（FR-REP-10・11）。
// 値の取得・時計の参照はしない。時刻はスナップショットの At のオフセットで出す。

// usageView は「使用状況」の節。Line があれば節の中身はその 1 行だけ（無効・未取得・取得できず）。
type usageView struct {
	Line     string
	Sections []usageSection
	Stale    string // stale のとき、節の末尾に足す 1 行
}

type usageSection struct {
	Name string
	Body string // markdown の表、または「- なし」
}

// pct は整数の率。
func pct(n int) string { return strconv.Itoa(n) + "%" }

func (w words) cpuText(c host.CPU) string {
	if !c.Available {
		return w.unavailable
	}
	return pct(c.UsedPercent)
}

func (w words) swapText(s host.Swap) string {
	if !s.Available {
		return w.unavailable
	}
	return pct(s.UsedPercent) // スワップ無しは 0%（取得できずとは区別する）
}

// diskText は `/ 73%（空き 120 GB）、/mnt/nvme 78%（空き 830 GB）`。取れないマウントは `/mnt/nvme 取得できず`。
func (w words) diskText(disks []host.Disk) string {
	if len(disks) == 0 {
		return emptyCell
	}
	parts := make([]string, 0, len(disks))
	for _, d := range disks {
		if !d.Available {
			parts = append(parts, cell(d.Mount)+" "+w.unavailable)
			continue
		}
		free := w.freePre + strconv.FormatFloat(float64(int64(d.FreeGB+0.5)), 'f', -1, 64) + " GB" + w.freePost
		parts = append(parts, cell(d.Mount)+" "+pct(d.UsedPercent)+w.paren(free))
	}
	return strings.Join(parts, w.partSep)
}

// version は「名前 版」（版が無ければ名前だけ）。
func version(name, v string) string {
	if v == "" {
		return name
	}
	return name + " " + v
}

// statusNote は更新確認の状態の言葉。latest は「新しい版 X あり」の X。
func (w words) statusNote(status, latest string) string {
	switch status {
	case reportvalues.OmpUpToDate:
		return w.upToDate
	case reportvalues.OmpUpdateAvailable:
		return w.updateAvail + latest + w.updateAvailAfter
	case reportvalues.OmpPending:
		return w.pending
	case reportvalues.OmpDisabled:
		return w.disabled
	}
	return w.unavailable
}

// updatesText は `omp 18.14.2（最新）／ swarm-console v0.1.5（最新）`。
func (w words) updatesText(snap *pane.Snapshot) string {
	o, u := snap.Omp, snap.Update
	omp := version("omp", o.Current) + w.paren(w.statusNote(o.Status, o.Latest))
	self := version("swarm-console", u.Current) + w.paren(w.statusNote(u.Status, tagOrEmpty(u.Latest)))
	return omp + w.listSep + self
}

// loginText は `omp 不明 / claude 期限切れ / codex 期限間近`。
func (w words) loginText(auth []host.Auth) string {
	if len(auth) == 0 {
		return emptyCell
	}
	parts := make([]string, 0, len(auth))
	for _, a := range auth {
		parts = append(parts, a.Agent+" "+w.authWord(a.Status))
	}
	return strings.Join(parts, " / ")
}

// num は小数 1 位に丸めた値（整数なら小数なし）。
func num(f float64) string { return strconv.FormatFloat(round1(f), 'f', -1, 64) }

// windowText は枠 1 つ：`48%（リセット 2026-11-01 07:00）`・`0 USD`・`100%（上限到達、リセット …）`。取れなければ「取得できず」。
func (w words) windowText(win reportvalues.Window, at time.Time) string {
	if !win.Available {
		return w.unavailable
	}
	s := num(win.Used) + "%"
	if win.Unit == reportvalues.UnitUSD {
		s = num(win.Used) + " USD"
	}
	var notes []string
	if win.Status == reportvalues.WindowExhausted {
		notes = append(notes, w.exhausted)
	}
	if win.HasReset() {
		notes = append(notes, w.resets+" "+win.ResetsAt.In(at.Location()).Format(minuteLayout))
	}
	if len(notes) > 0 {
		s += w.paren(strings.Join(notes, w.partSep))
	}
	return s
}

// table は markdown の表。セルは cell() で整え済みのものを渡す。
func table(head []string, rows [][]string) string {
	var b strings.Builder
	line := func(cells []string) { b.WriteString("| " + strings.Join(cells, " | ") + " |\n") }
	line(head)
	sep := make([]string, len(head))
	for i := range sep {
		sep[i] = "---"
	}
	line(sep)
	for _, r := range rows {
		line(r)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (w words) accountSection(name string, accts []reportvalues.Account, at time.Time) usageSection {
	if len(accts) == 0 {
		return usageSection{Name: name, Body: "- " + w.none}
	}
	rows := make([][]string, 0, len(accts))
	for _, a := range accts {
		rows = append(rows, []string{cell(a.Name), cell(w.windowText(a.FiveHour, at)), cell(w.windowText(a.SevenDay, at))})
	}
	return usageSection{Name: name, Body: table([]string{w.account, w.fiveHour, w.sevenDay}, rows)}
}

func (w words) cursorSection(accts []reportvalues.CursorAccount, at time.Time) usageSection {
	if len(accts) == 0 {
		return usageSection{Name: "Cursor", Body: "- " + w.none}
	}
	rows := make([][]string, 0, len(accts))
	for _, a := range accts {
		rows = append(rows, []string{cell(a.Name), cell(a.Window), cell(w.windowText(a.Models, at))})
	}
	return usageSection{Name: "Cursor", Body: table([]string{w.account, w.window, w.used}, rows)}
}

// usageView は「使用状況」の節（FR-REP-12・§5.3）。disabled・pending・unavailable は 1 行、stale は表のあとに「取得できず（最後の成功: 時刻）」。
func (w words) usageView(snap *pane.Snapshot) usageView {
	u, at := snap.Usage, snap.At
	switch u.Status {
	case reportvalues.StatusDisabled:
		return usageView{Line: w.disabled}
	case reportvalues.StatusPending:
		return usageView{Line: w.pending}
	case reportvalues.StatusOK, reportvalues.StatusStale:
	default:
		return usageView{Line: w.unavailable}
	}
	v := usageView{Sections: []usageSection{
		w.accountSection("Claude", u.Claude, at), w.cursorSection(u.Cursor, at), w.accountSection("Codex", u.Codex, at),
	}}
	if u.Status == reportvalues.StatusStale {
		v.Stale = w.unavailable
		if !u.FetchedAt.IsZero() {
			v.Stale += w.paren(w.lastSuccess + u.FetchedAt.In(at.Location()).Format(minuteLayout))
		}
	}
	return v
}
