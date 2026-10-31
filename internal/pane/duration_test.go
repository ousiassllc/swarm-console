package pane

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"}, {0, "0s"}, {20 * time.Second, "20s"}, {59*time.Second + 900*time.Millisecond, "59s"},
		{time.Minute, "1m"}, {42 * time.Minute, "42m"}, {59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h00m"}, {65 * time.Minute, "1h05m"}, {26*time.Hour + 3*time.Minute, "26h03m"},
	}
	for _, tc := range tests {
		if got := FormatDuration(tc.d); got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
