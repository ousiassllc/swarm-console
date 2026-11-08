package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// pair・token list を run() から呼ぶ（配線・終了コード・usage。中身は internal/pair・internal/app のテスト）。
func TestPairAndTokenListThroughRun(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml") // 無いので pair は token を発行せず終了コード 1
	if code, stdout, _ := runCLI(t, "pair", "--config", cfg); code != 1 || stdout != "" {
		t.Errorf("pair: exit %d stdout=%q", code, stdout)
	}
	for args, want := range map[string]int{"pair --sessions ,": 2, "pair --scope admin": 2, "pair --scope": 2, "pair x": 2, "pair --help": 0, "token list x": 2, "token list --config " + cfg: 0} {
		if code, stdout, _ := runCLI(t, strings.Fields(args)...); code != want || stdout != "" {
			t.Errorf("%s: exit %d stdout=%q, want %d", args, code, stdout, want)
		}
	}
	if _, out, _ := runCLI(t, "help"); !strings.Contains(out, "swarm-console pair") || !strings.Contains(out, "token list") {
		t.Errorf("help に pair / token list がありません:\n%s", out)
	}
}
