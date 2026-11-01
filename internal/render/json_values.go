package render

import (
	"time"

	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// このファイルは、`report` の JSON と `host_status` が共有する「足した値」の形（API 仕様 §5.5）。
// 値はスナップショットに入ったものだけを使い、時刻はスナップショットの At のオフセットで出す（FR-REP-7・10）。

// CPUJSON は host.cpu。
type CPUJSON struct {
	Available   bool `json:"available"`
	UsedPercent *int `json:"used_percent"`
}

// SwapJSON は host.swap。
type SwapJSON struct {
	Available   bool     `json:"available"`
	TotalGB     *float64 `json:"total_gb"`
	UsedGB      *float64 `json:"used_gb"`
	UsedPercent *int     `json:"used_percent"`
}

// DiskJSON は host.disk の 1 要素。
type DiskJSON struct {
	Mount       string   `json:"mount"`
	Available   bool     `json:"available"`
	FreeGB      *float64 `json:"free_gb"`
	TotalGB     *float64 `json:"total_gb"`
	UsedPercent *int     `json:"used_percent"`
}

// WindowJSON は使用状況の 1 枠。取れない枠は available:false でほかは null。
type WindowJSON struct {
	Available bool     `json:"available"`
	Used      *float64 `json:"used"`
	Unit      *string  `json:"unit"`
	Status    *string  `json:"status"`
	ResetsAt  *string  `json:"resets_at"`
}

// UsageAccountJSON は Claude・Codex の 1 アカウント。
type UsageAccountJSON struct {
	Account  string     `json:"account"`
	FiveHour WindowJSON `json:"five_hour"`
	SevenDay WindowJSON `json:"seven_day"`
}

// UsageCursorJSON は Cursor の 1 アカウント。
type UsageCursorJSON struct {
	Account string     `json:"account"`
	Window  *string    `json:"window"`
	Models  WindowJSON `json:"models"`
}

// UsageJSON は usage。
type UsageJSON struct {
	Status    string             `json:"status"`
	Reason    *string            `json:"reason"`
	FetchedAt *string            `json:"fetched_at"`
	Claude    []UsageAccountJSON `json:"claude"`
	Cursor    []UsageCursorJSON  `json:"cursor"`
	Codex     []UsageAccountJSON `json:"codex"`
}

// OmpJSON は omp。
type OmpJSON struct {
	Status    string  `json:"status"`
	Current   *string `json:"current"`
	Latest    *string `json:"latest"`
	Reason    *string `json:"reason"`
	CheckedAt *string `json:"checked_at"`
}

// UpdateJSON は update（swarm-console 自身。API 仕様 §5.3）。
type UpdateJSON struct {
	Status    string    `json:"status"`
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt string    `json:"checked_at,omitempty"`
	Apply     ApplyJSON `json:"apply"`
}

// ApplyJSON は update.apply。
type ApplyJSON struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	At      string `json:"at,omitempty"`
	From    string `json:"from,omitempty"`
	Version string `json:"version,omitempty"`
}

// AuthEntryJSON は auth.agents の 1 要素（agent_auth_status と同じ）。
type AuthEntryJSON struct {
	Agent     string  `json:"agent"`
	Status    string  `json:"status"`
	ExpiresAt *string `json:"expires_at"`
}

// AuthJSON は auth。
type AuthJSON struct {
	Agents []AuthEntryJSON `json:"agents"`
}

// timeStr は t を at と同じオフセットの RFC 3339 にする。ゼロなら null。
func timeStr(t, at time.Time) *string {
	if t.IsZero() {
		return nil
	}
	return new(t.In(at.Location()).Format(rfc3339))
}

// NewCPUJSON は snap の host.cpu。
func NewCPUJSON(snap *pane.Snapshot) CPUJSON {
	c := snap.Host.CPU
	if !c.Available {
		return CPUJSON{}
	}
	return CPUJSON{Available: true, UsedPercent: new(c.UsedPercent)}
}

// NewSwapJSON は snap の host.swap。
func NewSwapJSON(snap *pane.Snapshot) SwapJSON {
	s := snap.Host.Swap
	if !s.Available {
		return SwapJSON{}
	}
	return SwapJSON{Available: true, TotalGB: new(round1(s.TotalGB)), UsedGB: new(round1(s.UsedGB)), UsedPercent: new(s.UsedPercent)}
}

// NewDiskJSON は snap の host.disk（disk_mounts の順）。
func NewDiskJSON(snap *pane.Snapshot) []DiskJSON {
	out := make([]DiskJSON, 0, len(snap.Host.Disk))
	for _, d := range snap.Host.Disk {
		if !d.Available {
			out = append(out, DiskJSON{Mount: d.Mount})
			continue
		}
		out = append(out, DiskJSON{
			Mount: d.Mount, Available: true, FreeGB: new(round1(d.FreeGB)), TotalGB: new(round1(d.TotalGB)), UsedPercent: new(d.UsedPercent),
		})
	}
	return out
}

func newWindowJSON(w reportvalues.Window, at time.Time) WindowJSON {
	if !w.Available {
		return WindowJSON{}
	}
	return WindowJSON{Available: true, Used: new(w.Used), Unit: new(w.Unit), Status: new(w.Status), ResetsAt: timeStr(w.ResetsAt, at)}
}

func newAccountsJSON(in []reportvalues.Account, at time.Time) []UsageAccountJSON {
	out := make([]UsageAccountJSON, 0, len(in))
	for _, a := range in {
		out = append(out, UsageAccountJSON{Account: a.Name, FiveHour: newWindowJSON(a.FiveHour, at), SevenDay: newWindowJSON(a.SevenDay, at)})
	}
	return out
}

// NewUsageJSON は snap の usage。
func NewUsageJSON(snap *pane.Snapshot) UsageJSON {
	u, at := snap.Usage, snap.At
	out := UsageJSON{
		Status: u.Status, Reason: nilIfEmpty(u.Reason), FetchedAt: timeStr(u.FetchedAt, at),
		Claude: newAccountsJSON(u.Claude, at), Codex: newAccountsJSON(u.Codex, at), Cursor: make([]UsageCursorJSON, 0, len(u.Cursor)),
	}
	for _, c := range u.Cursor {
		out.Cursor = append(out.Cursor, UsageCursorJSON{Account: c.Name, Window: nilIfEmpty(c.Window), Models: newWindowJSON(c.Models, at)})
	}
	return out
}

// NewOmpJSON は snap の omp。
func NewOmpJSON(snap *pane.Snapshot) OmpJSON {
	o := snap.Omp
	return OmpJSON{
		Status: o.Status, Current: nilIfEmpty(o.Current), Latest: nilIfEmpty(o.Latest),
		Reason: nilIfEmpty(o.Reason), CheckedAt: timeStr(o.CheckedAt, snap.At),
	}
}

func tagOrEmpty(s string) string {
	if updatecheck.ValidTag(s) {
		return s
	}
	return ""
}

func timeOrEmpty(t, at time.Time) string {
	if s := timeStr(t, at); s != nil {
		return *s
	}
	return ""
}

// NewUpdateJSON は snap の update。latest・from・version は vX.Y.Z に合うときだけ出す。
func NewUpdateJSON(snap *pane.Snapshot) UpdateJSON {
	u, at := snap.Update, snap.At
	return UpdateJSON{
		Status: u.Status, Current: u.Current, Latest: tagOrEmpty(u.Latest), Reason: u.Reason, CheckedAt: timeOrEmpty(u.CheckedAt, at),
		Apply: ApplyJSON{
			State: u.Apply.State, Reason: u.Apply.Reason, At: timeOrEmpty(u.Apply.At, at),
			From: tagOrEmpty(u.Apply.From), Version: tagOrEmpty(u.Apply.Version),
		},
	}
}

// NewAuthJSON は snap の auth（omp・claude・codex の順）。
func NewAuthJSON(snap *pane.Snapshot) AuthJSON {
	out := AuthJSON{Agents: make([]AuthEntryJSON, 0, len(snap.Auth))}
	for _, a := range snap.Auth {
		out.Agents = append(out.Agents, AuthEntryJSON{Agent: a.Agent, Status: a.Status, ExpiresAt: authExpires(a, snap.At)})
	}
	return out
}

func authExpires(a host.Auth, at time.Time) *string {
	if a.ExpiresAt == nil {
		return nil
	}
	return timeStr(*a.ExpiresAt, at)
}
