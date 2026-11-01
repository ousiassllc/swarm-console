package tools

import (
	"time"

	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// UpdateSource は host_status.update の元データを返す（FR-UPD-9）。呼び出しで GitHub へは通信しない。
type UpdateSource interface {
	UpdateInfo() UpdateInfo
}

// UpdateInfo は新しい版の確認の結果と自己更新の状況。文字列は serve が決めた固定の語彙で、
// Latest・Apply.From・Apply.Version だけは GitHub 由来の版なので、出す前に vX.Y.Z の形を確かめる。
type UpdateInfo struct {
	Status    string // up_to_date・update_available・unknown・pending・disabled
	Current   string // 動いているバイナリの版（版を入れていないビルドは dev）
	Latest    string
	Reason    string // Status が unknown のとき
	CheckedAt time.Time
	Apply     ApplyInfo
}

// ApplyInfo は自己更新の状況（FR-UPD-20）。
type ApplyInfo struct {
	State   string // off・idle・probation・failed・blocked
	Reason  string // failed のとき
	At      time.Time
	From    string // probation のとき
	Version string // blocked のとき
}

type updateJSON struct {
	Status    string    `json:"status"`
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CheckedAt string    `json:"checked_at,omitempty"`
	Apply     applyJSON `json:"apply"`
}

type applyJSON struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	At      string `json:"at,omitempty"`
	From    string `json:"from,omitempty"`
	Version string `json:"version,omitempty"`
}

// newUpdateJSON は API 仕様 §5.3 の形にする。latest・from・version は vX.Y.Z に合うときだけ出す。
func newUpdateJSON(u UpdateInfo) updateJSON {
	out := updateJSON{
		Status: u.Status, Current: u.Current, Latest: tagOrEmpty(u.Latest), Reason: u.Reason, CheckedAt: formatTime(u.CheckedAt),
		Apply: applyJSON{
			State: u.Apply.State, Reason: u.Apply.Reason, At: formatTime(u.Apply.At),
			From: tagOrEmpty(u.Apply.From), Version: tagOrEmpty(u.Apply.Version),
		},
	}
	return out
}

func tagOrEmpty(s string) string {
	if updatecheck.ValidTag(s) {
		return s
	}
	return ""
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}
