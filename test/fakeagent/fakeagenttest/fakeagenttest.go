// Package fakeagenttest は擬似エージェント（test/fakeagent）をテストから使う補助を提供する。
package fakeagenttest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const buildTimeout = 3 * time.Minute

// Names は Install が binDir に置くコマンド名（アダプタの process_match に当たる名前）。
var Names = []string{"omp", "claude", "codex"}

func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod が見つかりません")
		}
		dir = parent
	}
}

// Install は擬似エージェントを go build し、omp・claude・codex の名前で dir/bin に置いて binDir を返す。
// 名前つきのシンボリックリンクなので、PATH に binDir を置いて `omp idle` と起動すると /proc/<pid>/cmdline の
// argv[0] が omp になり、アダプタの MatchProcess に当たる。
func Install(t testing.TB, dir string) (binDir string) {
	t.Helper()
	binDir = filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "fakeagent")
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", exe, "./test/fakeagent") //nolint:gosec // 引数は固定の文字列と t.TempDir 由来のパスだけ
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fakeagent をビルドできません: %v\n%s", err, out)
	}
	for _, n := range Names {
		if err := os.Symlink(exe, filepath.Join(binDir, n)); err != nil {
			t.Fatal(err)
		}
	}
	return binDir
}

// Event は --log に残った出来事 1 件。
type Event struct {
	Kind string // PASTE・CUSTOM・PICK・SUBMIT・ESCAPE
	N    int    // PICK の番号
	Text string // PASTE・CUSTOM の本文（貼り付けの CR は LF にそろえてある）
}

// ReadLog は --log のファイルを読む。ファイルがまだなければ空を返す。
func ReadLog(path string) ([]Event, error) {
	f, err := os.Open(path) //nolint:gosec // テストが渡す --log のパスを読む
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var out []Event
	for {
		head, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) && head == "" {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("ログの途中で切れています: %w", err)
		}
		kind, arg, _ := strings.Cut(strings.TrimSuffix(head, "\n"), " ")
		e := Event{Kind: kind}
		switch kind {
		case "PASTE", "CUSTOM":
			n, err := strconv.Atoi(arg)
			if err != nil {
				return out, fmt.Errorf("ログの長さが読めません: %q", head)
			}
			body := make([]byte, n+1)
			if _, err := io.ReadFull(r, body); err != nil {
				return out, fmt.Errorf("ログの本文が足りません: %w", err)
			}
			e.Text = string(body[:n])
		case "PICK":
			if e.N, err = strconv.Atoi(arg); err != nil {
				return out, fmt.Errorf("ログの番号が読めません: %q", head)
			}
		}
		out = append(out, e)
	}
}
