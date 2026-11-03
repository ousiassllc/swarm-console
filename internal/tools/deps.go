package tools

import (
	"errors"
	"io"
	"log/slog"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

// New はツール 17 個を組み立てる。必須の依存が無ければエラー。
func New(d toolkit.Deps) (*Registry, error) {
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
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &toolkit.Env{Deps: d, Detect: d.Safety.DetectSecret}
	return newRegistry(e), nil
}
