// Package config は config.toml・profiles.toml の読み込み・検証・既定値を扱う（ER §5・§6）。
package config

import (
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Config は検証済みの config.toml。Load（別ファイル）が組み立てる。
// 期間・パスはすべて既定値適用・`~` 展開・Clean 済み。
type Config struct {
	ListenAddr     string
	TmuxSocket     string
	Sessions       []string
	DefaultSession string
	DefaultLang    string // "ja" | "en"

	PollInterval     time.Duration
	SnapshotTTL      time.Duration
	StalledAfter     time.Duration
	LayoutWarningTTL time.Duration
	AcceptTimeout    time.Duration
	StartTimeout     time.Duration
	LockTimeout      time.Duration

	// UpdateCheck は serve の定期確認（FR-UPD-7）、UpdateAuto は見つけた新しい版への自己更新（FR-UPD-19）。
	// UpdateCheck が false なら UpdateAuto に関わらず確認も入れ替えもしない。
	UpdateCheck         bool
	UpdateCheckInterval time.Duration // 1h 以上
	UpdateAuto          bool

	// UsageCheck は serve が `omp usage --json` で使用状況を取るか（FR-REP-12・NFR-OPS-12）。UsageInterval は 1m 以上。
	// omp の更新確認（FR-REP-15）は UpdateCheck・UpdateCheckInterval に従う。
	UsageCheck    bool
	UsageInterval time.Duration

	TitlePrefix  string
	AllowedRoots []string
	DeniedPaths  []string
	FileRoots    []string
	DiskMounts   []string
	ReportDir    string

	// FileGetRoots は file_get が読んでよい根の追加分（既定は空。常に許すペインの cwd・ReportDir に足す）。
	// FileGetMaxBytes は file_get が返す 1 ファイルの上限（バイト。既定 DefaultFileGetMaxBytes）。
	FileGetRoots    []string
	FileGetMaxBytes int64

	// PaneNames は固定名。キーは "<セッション>:<ウィンドウ>.<ペイン番号>"、値は名前。
	PaneNames map[string]string
	// Agents は [agents.<name>] の上書き。キーは omp・claude・codex。
	Agents map[string]agent.Overrides

	// 設定フォルダ・データの場所（config.toml のキーではない。Load が決める）。
	ConfigDir    string // ~/.config/swarm-console
	TokensPath   string // ConfigDir/tokens.toml
	TemplatesDir string // ConfigDir/templates
	NotesPath    string // ~/.local/share/swarm-console/notes.json
	ProfilesPath string // ConfigDir/profiles.toml
}

// Profile は dispatch の条件のひな形 1 つ。
type Profile struct {
	Name  string
	Rules []string
}

// Profiles は profiles.toml（または組み込みの初期値）。
type Profiles struct {
	Common   []string
	Profiles map[string]Profile
}
