package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

// runServe は設定を読んで MCP サーバーを動かす。ログは slog の JSON Lines を標準エラーへ（監査ログも同じ出力）。
func runServe(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath, err := defaultConfig(fs)
	if err != nil {
		return err
	}
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError("serve に位置引数は要りません")
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	env := updateEnv
	if env.Home == "" {
		if env.Home, err = os.UserHomeDir(); err != nil {
			return fmt.Errorf("ホームフォルダを決められません: %w", err)
		}
	}
	// 自己更新の試運転は、設定の読み込みより前に判定する（設定エラーでも起動の回数として数える。FR-UPD-17）。
	var clk clock.Clock = clock.Real{}
	if env.Clock != nil {
		clk = env.Clock
	}
	probation := selfupdate.Probation(selfupdate.Dir(env.Home), version, clk, logger)
	if probation.Rollback {
		return errSilent // 旧バイナリへ戻した。ログは出してある。終了コード 1（systemd が旧バイナリを起動する）
	}
	a, err := app.New(*configPath, version, logger, app.WithTrial(probation.Trial), app.WithUpdateEnv(env))
	if err != nil {
		return err
	}
	logger.Info("起動します", "version", version, "listen_addr", a.Cfg.ListenAddr)
	return a.Run(ctx)
}

// defaultConfig は --config の flag を登録し、値のポインタを返す。既定は ~/.config/swarm-console/config.toml。
func defaultConfig(fs *flag.FlagSet) (*string, error) {
	def, err := app.DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return fs.String("config", def, "config.toml のパス"), nil
}
