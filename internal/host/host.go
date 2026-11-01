// Package host は負荷・メモリ・ディスク・/proc・認証期限の読み取りを扱う。
package host

import (
	"context"
	"time"
)

// Disk は監視マウントの使用状況。statfs に失敗したマウントは Available が false で残る（FR-REP-11）。
type Disk struct {
	Mount       string
	Available   bool
	FreeGB      float64 // 一般ユーザーが使える空き
	TotalGB     float64
	UsedPercent int // df の Use% と同じ（使用 ÷（使用 ＋ 一般ユーザーが使える空き）、切り上げ）
}

// CPU は CPU 使用率（/proc/stat の前回のサンプルとの差。FR-REP-13）。Available が false なら取得できず。
type CPU struct {
	Available   bool
	UsedPercent int
}

// Swap はスワップの使用量。スワップ無しは Available が true で TotalGB・UsedGB・UsedPercent が 0。
type Swap struct {
	Available   bool
	TotalGB     float64
	UsedGB      float64
	UsedPercent int
}

// Status は host_status・スナップショットの host。
type Status struct {
	Load       [3]float64 // 1・5・15 分
	CPUs       int
	MemTotalGB float64
	MemFreeGB  float64
	CPU        CPU
	Swap       Swap
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
	// Status は負荷・メモリ・スワップ・ディスク・稼働時間と、最後の CPU サンプルの値を返す。diskMounts は監視するマウント。
	Status(ctx context.Context, diskMounts []string) (Status, error)
	// SampleCPU は /proc/stat を読み、前回のサンプルとの差から CPU 使用率を更新する。サンプラーだけが poll_interval ごとに呼ぶ
	// （Status は最後のサンプルの値を返すだけなので、読み取りの呼び出しで値がぶれない）。
	SampleCPU()
	// ProcTree は pid を根とするプロセス木（pid 自身を含む）を返す。
	ProcTree(pid int) ([]Proc, error)
	// AuthStatus は agent の認証期限を読む。authFile が空なら組み込みの既定（P-16）。
	// ファイルが無い・読めない・形式が違うときはエラーではなく AuthUnknown。鍵の中身は返さない。
	AuthStatus(agent, authFile string, now time.Time) Auth
}
