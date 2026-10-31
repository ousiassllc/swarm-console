package pane

import (
	"fmt"
	"time"
)

// FormatDuration は期間を短く表す（API 仕様 §4）。1 分未満は `20s`、1 時間未満は `42m`、それ以上は `1h05m`。
// 負の値は 0 として扱う。
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}
