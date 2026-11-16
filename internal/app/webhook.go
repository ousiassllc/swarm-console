package app

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// newWebhook は [webhook] の url があるときだけ通知を作る。無ければ nil（何も作らず、何も送らない。FR-STATE-1〜7）。
// 戻り値の型を *webhook.Notifier にして、nil を pane.SnapshotObserver に入れたとき nil 判定が効かなくなるのを避ける。
func newWebhook(cfg *config.Config, version string, logger *slog.Logger) (*webhook.Notifier, error) {
	if !cfg.Webhook.Enabled() {
		return nil, nil
	}
	n, err := webhook.New(webhook.Options{
		Config: cfg.Webhook, ReportDir: cfg.ReportDir, StatePath: cfg.WebhookStatePath, Version: version, Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	logger.Info("webhook の通知を有効にしました", "max_attempts", cfg.Webhook.MaxAttempts, "retry_interval", cfg.Webhook.RetryInterval.String())
	return n, nil
}

// startWebhook は通知を始め、終了を待つ関数を返す。通知が無効なら何もしない。
func (a *App) startWebhook(ctx context.Context) (wait func()) {
	if a.webhook == nil {
		return func() {}
	}
	return a.webhook.Start(ctx)
}
