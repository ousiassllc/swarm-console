package tmux

import (
	"strings"
	"testing"
)

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		in      string
		warn    bool
		contain string
	}{
		{"tmux 3.2a", false, ""},
		{"tmux 3.0", false, ""},
		{"tmux 3.5-rc", false, ""},
		{"tmux next-3.5", false, ""},
		{"tmux master", false, ""},
		{"tmux 4.0", false, ""},
		{"tmux 2.9a", true, "3.x"},
		{"tmux 1.8", true, "paste-buffer -p"},
		{"tmux openbsd-7.4", false, ""},
		{"", true, "取得できません"},
		{"tmux", true, "判定できません"},
		{"tmux unknown", true, "判定できません"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := CheckVersion(tt.in)
			if (got != "") != tt.warn {
				t.Fatalf("CheckVersion(%q) = %q, 警告の有無が違う", tt.in, got)
			}
			if tt.contain != "" && !strings.Contains(got, tt.contain) {
				t.Fatalf("CheckVersion(%q) = %q, %q を含むはず", tt.in, got, tt.contain)
			}
		})
	}
}
