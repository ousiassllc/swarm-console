package toolkit

import (
	"io"
	"log/slog"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/safety"
)

// Deps はツールが使う依存。Registry・Ops・Notes・Host は差し替えられる（テストではフェイク）。
type Deps struct {
	Cfg       *config.Config
	Registry  pane.Registry
	Ops       pane.Ops
	Notes     notes.Store
	Safety    *safety.Policy
	Host      host.Host
	Profiles  *config.Profiles
	Templates *render.Templates
	Clock     clock.Clock // nil なら clock.Real
	Logger    *slog.Logger
	FileGet   *FileGetHooks // テスト専用。本番は nil
}

// FileGetHooks は file_get の競合（確認のあとの差し替え・肥大化）と読み取り量をテストで起こす・見るための差し込み点。
// 本番の配線では nil のまま使う。各フィールドは nil なら何もしない。
type FileGetHooks struct {
	// BeforeOpen は確認のあと、open の直前に呼ばれる（途中のディレクトリの差し替えを起こす）。
	BeforeOpen func(resolved string)
	// WrapReader は開いたファイルの確認のあと、読み取りの直前に呼ばれ、返した Reader から読む
	// （ファイルを大きくする・読んだバイト数を数える）。
	WrapReader func(r io.Reader) io.Reader
}

// Env はハンドラが共有する、既定値を埋めた依存。
type Env struct {
	Deps
	Detect notes.SecretDetector
}

// Now は現在時刻。
func (e *Env) Now() time.Time { return e.Clock.Now() }
