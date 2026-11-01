package main

import (
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	ok := []struct {
		args []string
		want options
	}{
		{[]string{"/x/omp", "idle"}, options{agent: "omp", scenario: "idle", tick: time.Second}},
		{[]string{"claude", "ask-custom", "--tick", "50ms", "--log", "/tmp/l"}, options{agent: "claude", scenario: "ask-custom", tick: 50 * time.Millisecond, log: "/tmp/l"}},
		{[]string{"fakeagent", "--log", "/tmp/l", "codex", "working"}, options{agent: "codex", scenario: "working", tick: time.Second, log: "/tmp/l"}},
	}
	for _, tt := range ok {
		got, err := parseArgs(tt.args)
		if err != nil || got != tt.want {
			t.Errorf("parseArgs(%v) = %+v, %v; want %+v", tt.args, got, err, tt.want)
		}
	}
	bad := [][]string{
		{"fakeagent"},
		{"fakeagent", "omp"},
		{"fakeagent", "vim", "idle"},
		{"omp", "nope"},
		{"codex", "ask-custom"},
		{"omp", "idle", "--tick", "0s"},
		{"omp", "idle", "extra"},
		{"omp", "idle", "--unknown"},
	}
	for _, args := range bad {
		if o, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%v) は拒否されるはず: %+v", args, o)
		}
	}
}
