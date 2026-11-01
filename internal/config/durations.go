package config

import (
	"fmt"
	"time"
)

// buildDurations は期間のキーを既定値つきで読み、下限・正の値を検査する（ER §5）。
func buildDurations(r *rawConfig, c *Config) error {
	items := []struct {
		key string
		src *string
		dst *time.Duration
		def time.Duration
		min time.Duration
	}{
		{"poll_interval", r.PollInterval, &c.PollInterval, defaultPollInterval, minPollInterval},
		{"snapshot_ttl", r.SnapshotTTL, &c.SnapshotTTL, defaultSnapshotTTL, minSnapshotTTL},
		{"stalled_after", r.StalledAfter, &c.StalledAfter, defaultStalledAfter, time.Nanosecond},
		{"layout_warning_ttl", r.LayoutWarningTTL, &c.LayoutWarningTTL, defaultLayoutWarningTTL, time.Nanosecond},
		{"accept_timeout", r.AcceptTimeout, &c.AcceptTimeout, defaultAcceptTimeout, time.Nanosecond},
		{"start_timeout", r.StartTimeout, &c.StartTimeout, defaultStartTimeout, time.Nanosecond},
		{"lock_timeout", r.LockTimeout, &c.LockTimeout, defaultLockTimeout, time.Nanosecond},
		{"update_check_interval", r.UpdateCheckInterval, &c.UpdateCheckInterval, defaultUpdateCheckInterval, minUpdateCheckInterval},
		{"usage_interval", r.UsageInterval, &c.UsageInterval, defaultUsageInterval, minUsageInterval},
	}
	for _, it := range items {
		*it.dst = it.def
		if it.src == nil {
			continue
		}
		d, err := time.ParseDuration(*it.src)
		if err != nil {
			return fmt.Errorf("%s は 5s・2m のような期間で書いてください", it.key)
		}
		if d < it.min {
			if it.min == time.Nanosecond {
				return fmt.Errorf("%s は 0 より大きい期間にしてください", it.key)
			}
			return fmt.Errorf("%s は %s 以上にしてください", it.key, it.min)
		}
		*it.dst = d
	}
	return nil
}
