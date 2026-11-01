package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipeFile は端末ではないファイル（パイプの片方）。
func pipeFile(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r
}

// 端末でないとき（標準入力か標準出力が端末でない）は、何も読まず何も書かず、理由と案内を標準エラーへ出して終了コード 1（FR-SET-6）。
func TestSetupNonTTY(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "sub", "config.toml")
	var out, errb bytes.Buffer
	err := runSetup(context.Background(), []string{"--config", cfg}, &out, &errb, pipeFile(t), pipeFile(t), "xterm-256color")
	if !errors.Is(err, errSilent) {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("標準出力に何か出ています: %q", out.String())
	}
	for _, want := range []string{"標準入力が端末ではありません", "listen_addr", "手で書く"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("標準エラーに %q がありません:\n%s", want, errb.String())
		}
	}
	if _, err := os.Stat(filepath.Dir(cfg)); err == nil {
		t.Error("フォルダまで作られています")
	}
}

func TestSetupRejectsPositionalArgs(t *testing.T) {
	var out, errb bytes.Buffer
	err := runSetup(context.Background(), []string{"extra"}, &out, &errb, pipeFile(t), pipeFile(t), "xterm")
	if !errors.Is(err, errUsage) {
		t.Fatalf("err = %v", err)
	}
}
