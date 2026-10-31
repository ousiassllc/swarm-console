package main

import (
	"context"
	"flag"
	"io"
	"log/slog"

	"github.com/ousiassllc/swarm-console/internal/app"
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
	a, err := app.New(*configPath, version, logger)
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
