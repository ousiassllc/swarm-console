package toolkit

import (
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
}

// Env はハンドラが共有する、既定値を埋めた依存。
type Env struct {
	Deps
	Detect notes.SecretDetector
}

// Now は現在時刻。
func (e *Env) Now() time.Time { return e.Clock.Now() }
