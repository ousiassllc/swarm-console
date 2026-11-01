package pane

import (
	"errors"
	"io"
	"log/slog"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/host"
)

// Deps は Registry・Ops が使う依存。すべてインターフェース越しで、テストではフェイクに差し替える。
type Deps struct {
	Cfg      *config.Config
	Tmux     Tmux
	Host     host.Host
	Adapters map[string]agent.Adapter // キーはアダプタ名（omp・claude・codex）
	Notes    NoteStore
	// Values はレポートに足す値の元（serve が持つ最後の結果）。nil なら pending のまま。
	Values Values
	Clock  clock.Clock // nil なら clock.Real
	FS     ReportFS    // nil なら OSReportFS
	Logger *slog.Logger

	// VersionWarn は tmux -V の出力から警告文を作る（tmux.CheckVersion を渡す）。nil なら確認しない。
	VersionWarn func(version string) string
	// CheckText は貼り付け本文の検証（safety.CheckText を渡す）。nil なら内蔵の簡易検査。Ops が使う。
	CheckText func(text string) error
}

// WithDefaults は nil の Clock・FS・Logger を既定で埋めた Deps を返す。必須の依存が無ければエラー。
func (d Deps) WithDefaults() (Deps, error) {
	switch {
	case d.Cfg == nil:
		return d, errors.New("pane: Cfg が必要です")
	case d.Tmux == nil:
		return d, errors.New("pane: Tmux が必要です")
	case d.Host == nil:
		return d, errors.New("pane: Host が必要です")
	case d.Notes == nil:
		return d, errors.New("pane: Notes が必要です")
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.FS == nil {
		d.FS = OSReportFS{}
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return d, nil
}
