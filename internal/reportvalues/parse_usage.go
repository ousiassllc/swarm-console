package reportvalues

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"
)

// errNoReports は reports が無い出力（形が違う）。
var errNoReports = errors.New("reports がありません")

// omp の provider 名。ほかは捨てる（report-values.md §2.1）。
const (
	providerClaude = "anthropic"
	providerCodex  = "openai-codex"
	providerCursor = "cursor"
)

// cursorLabel は Cursor で出す枠（ほかの枠は出さない）。
const cursorLabel = "Cursor Models"

// 出力の読み取り用の形。読まないキー（accountId・orgId など）は構造体に持たない。
type usageDoc struct {
	GeneratedAt *float64      `json:"generatedAt"`
	Reports     []usageReport `json:"reports"`
	// 使用量が取れなかったアカウント。要素の形は reports と同じ provider・metadata.email だけ読む。
	Without []usageReport `json:"accountsWithoutUsage"`
}

type usageReport struct {
	Provider string `json:"provider"`
	Metadata struct {
		Email string `json:"email"`
	} `json:"metadata"`
	Limits []usageLimit `json:"limits"`
}

type usageLimit struct {
	Label string `json:"label"`
	Scope struct {
		Shared bool `json:"shared"`
	} `json:"scope"`
	Window struct {
		ID       string   `json:"id"`
		ResetsAt *float64 `json:"resetsAt"`
	} `json:"window"`
	Amount struct {
		Used *float64 `json:"used"`
		Unit string   `json:"unit"`
	} `json:"amount"`
	Status string `json:"status"`
}

// ParseUsage は `omp usage --json` の出力を読む。形が合わなければエラー（呼び出し側は parse の失敗にする）。
// 返す Usage の Status は StatusOK、FetchedAt は generatedAt（無ければゼロ。呼び出し側が補う）。
func ParseUsage(out []byte) (Usage, error) {
	var doc usageDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		return Usage{}, err
	}
	if doc.Reports == nil {
		return Usage{}, errNoReports
	}
	u := Usage{Status: StatusOK, Claude: []Account{}, Cursor: []CursorAccount{}, Codex: []Account{}}
	if doc.GeneratedAt != nil {
		u.FetchedAt = epochMillis(*doc.GeneratedAt)
	}
	byProvider := map[string][]usageReport{}
	seen := map[string]bool{} // reports にあった provider + email（accountsWithoutUsage の同じものは 1 つにする）
	known := func(r usageReport) bool {
		return r.Provider == providerClaude || r.Provider == providerCodex || r.Provider == providerCursor
	}
	for _, r := range doc.Reports { // omp が返したアカウントは全部残す（FR-REP-12。同じメールの別の認証も落とさない）
		if !known(r) {
			continue
		}
		seen[r.Provider+"\x00"+r.Metadata.Email] = r.Metadata.Email != ""
		byProvider[r.Provider] = append(byProvider[r.Provider], r)
	}
	for _, r := range doc.Without {
		if !known(r) || seen[r.Provider+"\x00"+r.Metadata.Email] {
			continue
		}
		byProvider[r.Provider] = append(byProvider[r.Provider], r)
	}
	for _, r := range namedReports(byProvider[providerClaude]) {
		u.Claude = append(u.Claude, buildAccount(r.name, r.rep))
	}
	for _, r := range namedReports(byProvider[providerCodex]) {
		u.Codex = append(u.Codex, buildAccount(r.name, r.rep))
	}
	for _, r := range namedReports(byProvider[providerCursor]) {
		u.Cursor = append(u.Cursor, buildCursor(r.name, r.rep))
	}
	sort.SliceStable(u.Claude, func(i, j int) bool { return u.Claude[i].Name < u.Claude[j].Name })
	sort.SliceStable(u.Codex, func(i, j int) bool { return u.Codex[i].Name < u.Codex[j].Name })
	sort.SliceStable(u.Cursor, func(i, j int) bool { return u.Cursor[i].Name < u.Cursor[j].Name })
	return u, nil
}

type namedReport struct {
	name string
	rep  usageReport
}

func namedReports(reps []usageReport) []namedReport {
	emails := make([]string, len(reps))
	for i, r := range reps {
		emails[i] = r.Metadata.Email
	}
	names := accountNames(emails)
	out := make([]namedReport, len(reps))
	for i, r := range reps {
		out[i] = namedReport{name: names[i], rep: r}
	}
	return out
}

func buildAccount(name string, r usageReport) Account {
	a := Account{Name: name}
	for _, l := range r.Limits {
		if !l.Scope.Shared {
			continue // tier つきの枠（fable など）は出さない
		}
		switch l.Window.ID {
		case "5h":
			a.FiveHour = toWindow(l)
		case "7d":
			a.SevenDay = toWindow(l)
		}
	}
	return a
}

func buildCursor(name string, r usageReport) CursorAccount {
	c := CursorAccount{Name: name}
	for _, l := range r.Limits {
		if l.Label == cursorLabel {
			// 枠の名前は出力の文字列なので、決まった形（monthly・weekly など）に合うものだけ出す（FR-REP-18）。
			if labelRE.MatchString(l.Window.ID) {
				c.Window = l.Window.ID
			}
			c.Models = toWindow(l)
			break
		}
	}
	return c
}

// toWindow は枠を読む。使用量が数値でない・単位が percent・usd 以外なら取得できず。
func toWindow(l usageLimit) Window {
	u := l.Amount.Used
	if u == nil || math.IsNaN(*u) || math.IsInf(*u, 0) || (l.Amount.Unit != UnitPercent && l.Amount.Unit != UnitUSD) {
		return Window{}
	}
	w := Window{Available: true, Used: math.Round(*u*10) / 10, Unit: l.Amount.Unit, Status: WindowUnknown}
	if l.Status == WindowOK || l.Status == WindowExhausted {
		w.Status = l.Status
	}
	if l.Window.ResetsAt != nil {
		w.ResetsAt = epochMillis(*l.Window.ResetsAt)
	}
	return w
}

func epochMillis(ms float64) time.Time {
	if ms <= 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return time.Time{}
	}
	return time.UnixMilli(int64(ms)).UTC()
}
