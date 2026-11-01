package tools

import (
	"errors"
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
	// Update は host_status.update の元（serve が持つ最後の確認結果と自己更新の状況。呼び出しで通信しない）。
	Update UpdateSource
	Clock  clock.Clock // nil なら clock.Real
	Logger *slog.Logger
}

// env はハンドラが共有する、既定値を埋めた依存。
type env struct {
	Deps
	detect notes.SecretDetector
}

// New はツール 17 個を組み立てる。必須の依存が無ければエラー。
func New(d Deps) (*Registry, error) {
	switch {
	case d.Cfg == nil:
		return nil, errors.New("tools: Cfg が必要です")
	case d.Registry == nil:
		return nil, errors.New("tools: Registry が必要です")
	case d.Ops == nil:
		return nil, errors.New("tools: Ops が必要です")
	case d.Notes == nil:
		return nil, errors.New("tools: Notes が必要です")
	case d.Safety == nil:
		return nil, errors.New("tools: Safety が必要です")
	case d.Host == nil:
		return nil, errors.New("tools: Host が必要です")
	case d.Profiles == nil:
		return nil, errors.New("tools: Profiles が必要です")
	case d.Templates == nil:
		return nil, errors.New("tools: Templates が必要です")
	case d.Update == nil:
		return nil, errors.New("tools: Update が必要です")
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &env{Deps: d, detect: d.Safety.DetectSecret}
	return newRegistry(e), nil
}

// now は現在時刻。
func (e *env) now() time.Time { return e.Clock.Now() }
