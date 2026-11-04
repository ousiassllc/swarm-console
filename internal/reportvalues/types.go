// Package reportvalues はレポートに足す値のうち、遅い取得（`omp usage --json`・`omp update --check`）を
// 決まった間隔で行い、最後の結果を不変の値として保持する（FR-REP-10〜15・18。集め方は docs/architecture/report-values.md）。
// 結果にメール・accountId・orgId・コマンド出力の文面を持たない。
package reportvalues

import "time"

// Usage.Status（API 仕様 §5.5）。
const (
	StatusOK          = "ok"          // 最後の取得が成功
	StatusStale       = "stale"       // 最後の取得が失敗。前の成功分を残す
	StatusUnavailable = "unavailable" // 成功が一度も無い
	StatusPending     = "pending"     // 起動後まだ取得していない
	StatusDisabled    = "disabled"    // usage_check = false
)

// 取得の失敗の理由（固定のコードだけ。コマンドの出力・エラー文・パスは持たない）。
const (
	ReasonNotFound = "not_found"
	ReasonTimeout  = "timeout"
	ReasonExec     = "exec"
	ReasonParse    = "parse"
)

// Omp.Status（API 仕様 §5.5）。
const (
	OmpUpToDate        = "up_to_date"
	OmpUpdateAvailable = "update_available"
	OmpUnknown         = "unknown"
	OmpPending         = "pending"
	OmpDisabled        = "disabled"
)

// 枠の status（`omp` の status。ほかの文字列は StatusUnknownWindow）。
const (
	WindowOK        = "ok"
	WindowExhausted = "exhausted"
	WindowUnknown   = "unknown"
)

// 枠の単位。
const (
	UnitPercent = "percent"
	UnitUSD     = "usd"
)

// Window は使用状況の 1 枠。Available が false なら、ほかの値は空（取れない枠。FR-REP-11）。
type Window struct {
	Available bool
	Used      float64 // 小数 1 位に丸め済み
	Unit      string  // UnitPercent・UnitUSD
	Status    string  // WindowOK・WindowExhausted・WindowUnknown
	ResetsAt  time.Time
}

// HasReset はリセット時刻があるか。
func (w Window) HasReset() bool { return !w.ResetsAt.IsZero() }

// Account の Reason（取れなかった・名前が作れなかった理由。どちらでもなければ ""）。
const (
	AccountWithoutUsage = "without_usage" // omp の accountsWithoutUsage から来た
	AccountNoEmail      = "no_email"      // メールが空で、名前を account-N にした
)

// Account は Claude・Codex の 1 アカウント。Name は names.go の規則だけ（メールの一部は出さない）。
// Reason は AccountWithoutUsage・AccountNoEmail・""。両方に当てはまるときは AccountWithoutUsage。
type Account struct {
	Name     string
	Reason   string
	FiveHour Window
	SevenDay Window
}

// CursorAccount は Cursor の 1 アカウント。Window は monthly・weekly など（枠が無ければ ""）。Reason は Account と同じ。
type CursorAccount struct {
	Name   string
	Reason string
	Window string
	Models Window
}

// Usage は使用状況。作ったあと変更しない（スナップショットが共有する）。
type Usage struct {
	Status    string
	Reason    string    // StatusStale・StatusUnavailable のときだけ
	FetchedAt time.Time // 最後に成功した取得。成功が無ければゼロ
	Claude    []Account
	Cursor    []CursorAccount
	Codex     []Account
}

// Omp は omp の更新確認。作ったあと変更しない。
type Omp struct {
	Status    string
	Current   string // X.Y.Z の形に合うものだけ。無ければ ""
	Latest    string
	Reason    string // OmpUnknown のときだけ
	CheckedAt time.Time
}
