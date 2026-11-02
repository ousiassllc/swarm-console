package pane

import (
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// UpdateInfo は swarm-console 自身の新しい版の確認の結果と自己更新の状況（host_status.update・report の update の元。
// FR-UPD-9・FR-REP-15）。文字列は serve が決めた固定の語彙で、Latest・Apply.From・Apply.Version だけは GitHub 由来の版なので、
// 出す前に vX.Y.Z の形を確かめる。
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

// Values はスナップショットを作るときに読む、serve が持つ最後の結果（FR-REP-10）。メモリの値を返すだけで、
// コマンドの実行・通信をしない。返す値は作ったあと変更されない。
type Values interface {
	Usage() reportvalues.Usage
	Omp() reportvalues.Omp
	UpdateInfo() UpdateInfo
}

// authStatuses は agent.Builtin のエージェントのログイン期限を now で判定する（スナップショットを作るときに 1 回読む。FR-REP-16）。
func (d Deps) authStatuses(now time.Time) []host.Auth {
	names := agent.Builtin()
	out := make([]host.Auth, 0, len(names))
	for _, name := range names {
		var authFile string
		if ov, ok := d.Cfg.Agents[name]; ok && ov.AuthFile != nil {
			authFile = *ov.AuthFile
		}
		out = append(out, d.Host.AuthStatus(name, authFile, now))
	}
	return out
}

// values は Values が無い（テスト・配線なし）ときの初期値。起動直後と同じ pending。
func (d Deps) values() (reportvalues.Usage, reportvalues.Omp, UpdateInfo) {
	if d.Values == nil {
		return reportvalues.Usage{Status: reportvalues.StatusPending, Claude: []reportvalues.Account{}, Cursor: []reportvalues.CursorAccount{}, Codex: []reportvalues.Account{}},
			reportvalues.Omp{Status: reportvalues.OmpPending}, UpdateInfo{Status: "pending", Apply: ApplyInfo{State: "off"}}
	}
	return d.Values.Usage(), d.Values.Omp(), d.Values.UpdateInfo()
}
