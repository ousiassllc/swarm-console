// Package host は負荷・メモリ・ディスク・/proc・認証期限の読み取りを扱う。
package host

import (
	"context"
	"time"
)

// Disk は監視マウントの空き容量。
type Disk struct {
	Mount  string
	FreeGB float64
}

// Status は host_status・スナップショットの host。
type Status struct {
	Load       [3]float64 // 1・5・15 分
	CPUs       int
	MemTotalGB float64
	MemFreeGB  float64
	Disk       []Disk
	Uptime     time.Duration
}

// Proc は /proc から読んだプロセス 1 つ。
type Proc struct {
	PID  int
	PPID int
	Argv []string // /proc/<pid>/cmdline を NUL で分けたもの
}

// AuthStatus の status の値（API 仕様 §5.3）。
const (
	AuthValid        = "valid"
	AuthExpired      = "expired"
	AuthExpiringSoon = "expiring_soon" // 48 時間以内に切れる
	AuthUnknown      = "unknown"
)

// Auth は agent_auth_status の 1 エージェント分。ExpiresAt が nil なら不明。
type Auth struct {
	Agent     string
	Status    string
	ExpiresAt *time.Time
}

// Host はドメイン層が使う読み取り口（テストでは差し替える）。
type Host interface {
	// Status は負荷・メモリ・ディスク・稼働時間を返す。diskMounts は監視するマウント。
	Status(ctx context.Context, diskMounts []string) (Status, error)
	// ProcTree は pid を根とするプロセス木（pid 自身を含む）を返す。
	ProcTree(pid int) ([]Proc, error)
	// AuthStatus は agent の認証期限を読む。authFile が空なら組み込みの既定（P-16）。
	// ファイルが無い・読めない・形式が違うときはエラーではなく AuthUnknown。鍵の中身は返さない。
	AuthStatus(agent, authFile string, now time.Time) Auth
}
