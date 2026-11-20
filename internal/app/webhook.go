package app

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/webhook"
)

// newWebhook は送り先（[webhook] の url か、[webhook.sessions."<名前>"] の url）が 1 つでもあるときだけ通知を作る。
// 無ければ nil（何も作らず、何も送らない。FR-STATE-1〜8）。
// 戻り値の型を *webhook.Notifier にして、nil を pane.SnapshotObserver に入れたとき nil 判定が効かなくなるのを避ける。
func newWebhook(cfg *config.Config, version string, logger *slog.Logger, un *pane.Unaccepted) (*webhook.Notifier, error) {
	if !cfg.Webhook.Enabled() {
		return nil, nil
	}
	n, err := webhook.New(webhook.Options{
		Config: cfg.Webhook, ReportDir: cfg.ReportDir, StatePath: cfg.WebhookStatePath, Version: version, Logger: logger, Unaccepted: un,
	})
	if err != nil {
		return nil, err
	}
	// 送り先は、どのセッションが [webhook] を使い、どのセッションが自分の表を使うか・送らないか（値は出さない）。
	var own, off []string
	for _, s := range cfg.Sessions {
		if t, ok := cfg.Webhook.Sessions[s]; ok {
			if t.Enabled() {
				own = append(own, s)
			} else {
				off = append(off, s)
			}
		}
	}
	attrs := []any{"default_url", cfg.Webhook.URL != "", "session_urls", own, "session_off", off}
	if cfg.Webhook.URL != "" { // 再送の設定は [webhook] の送り先のものだけを出す（セッションの表の値は、表ごとに違うので出さない）
		attrs = append(attrs, "max_attempts", cfg.Webhook.MaxAttempts, "retry_interval", cfg.Webhook.RetryInterval.String())
	}
	logger.Info("webhook の通知を有効にしました", attrs...)
	return n, nil
}

// startWebhook は通知を始め、終了を待つ関数を返す。通知が無効なら何もしない。
func (a *App) startWebhook(ctx context.Context) (wait func()) {
	if a.webhook == nil {
		return func() {}
	}
	return a.webhook.Start(ctx)
}
