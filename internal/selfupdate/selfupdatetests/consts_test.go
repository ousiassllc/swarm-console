package selfupdatetests

import "github.com/ousiassllc/swarm-console/internal/selfupdate"

// 置き場の 5 ファイルの名前（FR-UPD-15）。仕様書の値をそのまま書く（実装の定数に依らない）。
const (
	binName     = "swarm-console"
	newName     = "swarm-console.new"
	oldName     = "swarm-console.old"
	pendingName = "swarm-console.update-pending"
	failedName  = "swarm-console.update-failed"

	maxSumsBytes   = 64 << 10
	maxBinaryBytes = 64 << 20
)

// assetName は tag のバイナリのアセット名（P-57）。
func assetName(tag string) string { return "swarm-console_" + tag + "_linux_amd64" }

// pending・failed はマーカーの JSON（ER §8）。
type pending struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Boots int    `json:"boots"`
}

type failed struct {
	Version string `json:"version"`
	At      string `json:"at"`
}

// 型の別名（テスト内で短く書く）。
type (
	Options = selfupdate.Options
	Reason  = selfupdate.Reason
	State   = selfupdate.State
)
