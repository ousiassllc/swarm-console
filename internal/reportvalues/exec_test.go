package reportvalues

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeOmp は呼び出しの引数と標準入力を記録する偽の omp（テスト用の実行ファイル）を dir に作る。
// body はシェルの断片（標準出力に出す内容など）。
func fakeOmp(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fakeomp")
	script := "#!/bin/sh\necho \"$#:$*\" >> \"$0.args\"\ncat >> \"$0.stdin\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // テスト用の実行ファイル
		t.Fatal(err)
	}
	return path
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // テスト用の固定パス
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// NFR-SEC-7：実行する外部コマンドは引数固定の 2 つだけ。`command` に引数が書いてあっても使わない。
func TestExecRunnerFixedArgs(t *testing.T) {
	dir := t.TempDir()
	exe := fakeOmp(t, dir, "echo hello")
	r := &ExecRunner{Command: exe + " update --yes ; rm -rf /", Home: dir}
	for _, c := range []Command{CmdUsage, CmdUpdateCheck, CmdUsage} {
		out, reason := r.Run(context.Background(), c)
		if reason != "" || string(out) != "hello\n" {
			t.Fatalf("Run(%d) = %q %q", c, out, reason)
		}
	}
	want := []string{"2:usage --json", "2:update --check", "2:usage --json"}
	if got := readLines(t, exe+".args"); !reflect.DeepEqual(got, want) {
		t.Fatalf("呼び出しの引数 = %v, want %v", got, want)
	}
	// 標準入力は /dev/null（何も読めない）。
	if b, err := os.ReadFile(exe + ".stdin"); err != nil || len(b) != 0 { //nolint:gosec // テスト用の固定パス
		t.Fatalf("stdin = %q %v", b, err)
	}
	// 知らないコマンドは実行しない。
	if _, reason := r.Run(context.Background(), Command(99)); reason != ReasonExec || len(readLines(t, exe+".args")) != 3 {
		t.Fatalf("未知のコマンドを実行した: %q", reason)
	}
}

func TestExecRunnerFailures(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name   string
		body   string
		reason string
	}{
		{"終了コードが 0 以外", "echo x; exit 3", ReasonExec},
		{"打ち切り", "sleep 30", ReasonTimeout},
		{"出力が上限超え", "head -c 3000000 /dev/zero", ReasonExec},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe := fakeOmp(t, filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "")), tt.body)
			r := &ExecRunner{Command: exe, Home: dir, UsageTimeout: 300 * time.Millisecond}
			start := time.Now()
			out, reason := r.Run(context.Background(), CmdUsage)
			if reason != tt.reason || out != nil {
				t.Fatalf("out=%d reason=%q, want %q", len(out), reason, tt.reason)
			}
			if time.Since(start) > 10*time.Second {
				t.Fatal("打ち切りが効いていない")
			}
		})
	}
}

// 実行ファイルが無ければ not_found。絶対パスでなければ PATH と ~/.local/bin から探す。
func TestExecRunnerLookup(t *testing.T) {
	home := t.TempDir()
	r := &ExecRunner{Command: "no-such-omp-command", Home: home}
	if _, reason := r.Run(context.Background(), CmdUsage); reason != ReasonNotFound {
		t.Fatalf("reason = %q", reason)
	}
	if _, reason := (&ExecRunner{Command: filepath.Join(home, "missing"), Home: home}).Run(context.Background(), CmdUsage); reason != ReasonNotFound {
		t.Fatalf("絶対パスの不在: %q", reason)
	}
	fakeOmp(t, filepath.Join(home, ".local", "bin"), "echo local")
	out, reason := (&ExecRunner{Command: "fakeomp", Home: home}).Run(context.Background(), CmdUpdateCheck)
	if reason != "" || string(out) != "local\n" {
		t.Fatalf("~/.local/bin から見つからない: %q %q", out, reason)
	}
	if _, reason := (&ExecRunner{Command: "./fakeomp", Home: home}).Run(context.Background(), CmdUsage); reason != ReasonNotFound {
		t.Fatalf("相対パスは探さない: %q", reason)
	}
}
