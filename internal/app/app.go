// Package app は設定の読み込みから server の起動までの配線を持つ（cmd/swarm-console を薄く保つ）。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	_ "github.com/ousiassllc/swarm-console/internal/agent/allagents" // 組み込みアダプタの登録（FR-AD-4）
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/mcpapi"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/render"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
	"github.com/ousiassllc/swarm-console/internal/safety"
	"github.com/ousiassllc/swarm-console/internal/server"
	"github.com/ousiassllc/swarm-console/internal/tmux"
	"github.com/ousiassllc/swarm-console/internal/tools"
)

// DefaultConfigPath は --config の既定値（~/.config/swarm-console/config.toml）。
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームフォルダを決められません: %w", err)
	}
	return filepath.Join(home, ".config", "swarm-console", "config.toml"), nil
}

// TokensPath は config.toml と同じフォルダの tokens.toml（config.Load の ConfigDir と同じ決め方）。
// token add・revoke は設定全体の検証を要らないので、これでパスだけ決める。
func TokensPath(configPath string) (string, error) {
	dir, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return "", fmt.Errorf("設定フォルダを決められません: %w", err)
	}
	return filepath.Join(dir, "tokens.toml"), nil
}

// CheckTokenSessions は token add --sessions の名前が、configPath の config.toml の sessions に含まれるか確かめる。
// config.toml を読めない（無い・不正）ときもエラー（どのセッションが有効か分からないまま許可を書かない）。
func CheckTokenSessions(configPath string, sessions []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("--sessions を確かめるため config.toml を読めません: %w", err)
	}
	for _, s := range sessions {
		if !slices.Contains(cfg.Sessions, s) {
			return fmt.Errorf("--sessions のセッション %q は config.toml の sessions にありません", s)
		}
	}
	return nil
}

// App は組み立て済みの swarm-console。
type App struct {
	Cfg      *config.Config
	Registry *pane.SnapshotRegistry
	Server   *server.Server
	logger   *slog.Logger
	update   *updateRuntime
	values   *reportvalues.Collector
}

// New は設定を読み、テンプレート・プロファイル・アダプタ・tokens.toml を検証して一式を組み立てる。
// 構文エラーのテンプレート・不正な正規表現・権限が 600 でない tokens.toml・壊れた設定は、ここでエラーにして起動を拒否する。
func New(configPath, version string, logger *slog.Logger, opts ...Option) (*App, error) {
	var ao options
	for _, opt := range opts {
		opt(&ao)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	tpl, err := render.LoadTemplates(cfg.TemplatesDir)
	if err != nil {
		return nil, err
	}
	profiles, err := config.LoadProfiles(cfg.ProfilesPath)
	if err != nil {
		return nil, err
	}
	adapters, err := newAdapters(cfg)
	if err != nil {
		return nil, err
	}
	clk := clock.Real{}
	authn, err := auth.Open(cfg.TokensPath, clk, logger)
	if err != nil {
		return nil, fmt.Errorf("tokens.toml を開けません: %w", err)
	}
	if err := authn.CheckSessions(cfg.Sessions); err != nil {
		return nil, err
	}
	store, err := notes.NewStore(cfg.NotesPath, clk, notes.WithSecretDetector(safety.DetectSecret), notes.WithLogger(logger))
	if err != nil {
		return nil, fmt.Errorf("notes.json を開けません: %w", err)
	}
	// 最終報告の置き場（NFR-SEC-14 が許す書き込み先）。エージェントが書くので、先に作っておく。
	if err := os.MkdirAll(cfg.ReportDir, 0o700); err != nil {
		return nil, fmt.Errorf("最終報告のフォルダを作れません: %w", err)
	}
	policy := safety.NewPolicy(cfg)
	hostReader := host.NewReader(clk)
	up, err := newUpdateRuntime(cfg, version, logger, ao)
	if err != nil {
		return nil, err
	}
	col, err := newValuesCollector(cfg, logger, ao)
	if err != nil {
		return nil, err
	}
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: cfg, Tmux: tmux.New(cfg.TmuxSocket), Host: hostReader, Adapters: adapters, Notes: store,
		Values: valuesSource{col, up},
		Clock:  clk, Logger: logger, VersionWarn: tmux.CheckVersion, CheckText: policy.CheckText,
	})
	if err != nil {
		return nil, err
	}
	registry, err := tools.New(toolkit.Deps{
		Cfg: cfg, Registry: reg, Ops: ops.New(reg), Notes: store, Safety: policy, Host: hostReader,
		Profiles: profiles, Templates: tpl, Clock: clk, Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	o := mcpapi.Options{Tools: registry, Version: version, Audit: server.AuditFunc(logger)}
	srv, err := server.New(server.Options{
		ListenAddr: cfg.ListenAddr, Auth: authn, Logger: logger,
		Report: mcpapi.NewHTTPHandler(mcpapi.NewReportServer(o)),
		Ops:    mcpapi.NewHTTPHandler(mcpapi.NewOpsServer(o)),
	})
	if err != nil {
		return nil, err
	}
	return &App{Cfg: cfg, Registry: reg, Server: srv, logger: logger, update: up, values: col}, nil
}

// newAdapters は登録済みの全アダプタを、設定の上書きを適用して作る。正規表現が不正なら起動拒否。
func newAdapters(cfg *config.Config) (map[string]agent.Adapter, error) {
	out := map[string]agent.Adapter{}
	for _, name := range agent.Names() {
		a, err := agent.New(name, cfg.Agents[name])
		if err != nil {
			return nil, fmt.Errorf("アダプタ %s: %w", name, err)
		}
		out[name] = a
	}
	return out, nil
}

// valuesSource は pane.Values。omp の使用状況・更新確認（収集器）と、swarm-console 自身の更新状態（updateRuntime）を束ねる。
type valuesSource struct {
	*reportvalues.Collector
	up *updateRuntime
}

func (v valuesSource) UpdateInfo() pane.UpdateInfo { return v.up.UpdateInfo() }

// newValuesCollector は `omp usage --json`・`omp update --check` の収集器を作る（NFR-OPS-12）。
// 実行ファイルは `[agents.omp] command` の先頭の語（既定 omp）。テストは UpdateEnv で Runner を差し替える。
func newValuesCollector(cfg *config.Config, logger *slog.Logger, o options) (*reportvalues.Collector, error) {
	env := o.env
	home := env.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ホームフォルダを決められません: %w", err)
		}
		home = h
	}
	runner := env.ValuesRunner
	if runner == nil {
		var command string
		if ov, ok := cfg.Agents["omp"]; ok && ov.Command != nil {
			command = *ov.Command
		}
		runner = &reportvalues.ExecRunner{Command: command, Home: home}
	}
	var clk clock.Clock = clock.Real{}
	switch {
	case env.ValuesClock != nil:
		clk = env.ValuesClock
	case env.Clock != nil:
		clk = env.Clock
	}
	return reportvalues.New(reportvalues.Options{
		Runner: runner, Clock: clk, Logger: logger,
		UsageCheck: cfg.UsageCheck, UsageInterval: cfg.UsageInterval,
		OmpCheck: cfg.UpdateCheck, OmpInterval: cfg.UpdateCheckInterval,
		UsageFirstDelay: env.ValuesFirstDelay, OmpFirstDelay: env.ValuesFirstDelay,
	}), nil
}

// Run はサンプラー・HTTP サーバー・新しい版の確認（と自己更新）を動かす。ctx が終わるとサーバーは進行中の操作を待って止まり、
// 確認は通信を中断して止まり、そのあとサンプラーを止める。待ち受けに失敗したらエラーで戻る（systemd の Restart=always に任せる）。
// 自己更新で入れ替えたあとは、ctx が終わったときと同じ経路で止まり nil で戻る（終了コード 0。FR-UPD-16）。
func (a *App) Run(parent context.Context) error {
	ctx, exit := context.WithCancel(parent)
	defer exit()
	a.update.setExit(exit)
	samplerCtx, stopSampler := context.WithCancel(context.WithoutCancel(ctx))
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		if err := a.Registry.Run(samplerCtx); err != nil && samplerCtx.Err() == nil {
			a.logger.Error("サンプラーが止まりました", "err", err)
		}
	}()
	waitUpdate := a.update.start(ctx, a.Server.Ready())
	waitValues := a.values.Start(ctx)
	err := a.Server.Serve(ctx)
	exit() // 待ち受けの失敗でも、確認と自己更新の通信・omp の実行を止める
	waitUpdate()
	waitValues()
	stopSampler()
	<-samplerDone
	return err
}
