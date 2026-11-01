package main

import (
	"os"
	"path/filepath"
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
		{[]string{"omp", "ask-frozen", "--raw", "/tmp/k"}, options{agent: "omp", scenario: "ask-frozen", tick: time.Second, raw: "/tmp/k"}},
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

func TestAppendRawKeepsBytesAsIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.bin")
	if err := appendRaw("", []byte("x")); err != nil {
		t.Fatal(err) // path が空なら何もしない
	}
	for _, b := range [][]byte{[]byte("\x1b[A"), []byte("\r\n")} {
		if err := appendRaw(path, b); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path) //nolint:gosec // テストの一時ファイル
	if err != nil || string(got) != "\x1b[A\r\n" {
		t.Fatalf("raw = %q, %v", got, err)
	}
}
