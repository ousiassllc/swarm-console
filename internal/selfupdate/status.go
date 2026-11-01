package selfupdate

import (
	"time"
)

// State は host_status.update.apply.state（FR-UPD-20。off は設定から tools が決める）。
type State string

// State の値。
const (
	StateIdle      State = "idle"
	StateProbation State = "probation"
	StateFailed    State = "failed"
	StateBlocked   State = "blocked"
)

// Reason は入れ替えに至らなかった理由（FR-UPD-18）。URL・ヘッダー・応答本文・生のエラー文面は持たない。
type Reason string

// Reason の値。
const (
	ReasonUnsupportedPlatform Reason = "unsupported_platform"
	ReasonNotManaged          Reason = "not_managed"
	ReasonAssetMissing        Reason = "asset_missing"
	ReasonDownload            Reason = "download"
	ReasonChecksum            Reason = "checksum"
	ReasonWrite               Reason = "write"
)

// Apply は自己更新の状況。From は probation、Reason・At は failed、Version は blocked のときだけ入る。
type Apply struct {
	State   State
	From    string
	Reason  Reason
	At      time.Time
	Version string
}
