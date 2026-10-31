// Package harness は統合・受け入れテストの土台（実 tmux の専用ソケット＋擬似エージェント＋ app＋ MCP クライアント）。
// テストからだけ使う。tmux が無い環境ではスキップせず失敗させる（NFR-TEST-8: CI は tmux を入れる）。
package harness

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/test/fakeagent/fakeagenttest"
)

var (
	binOnce sync.Once
	binDir  string
	binRoot string // Main が消す
)

// Main は各テストパッケージの TestMain から呼ぶ。擬似エージェントのビルド先を終了時に片付ける。
func Main(m *testing.M) {
	code := m.Run()
	if binRoot != "" {
		_ = os.RemoveAll(binRoot)
	}
	os.Exit(code)
}

// Bin は擬似エージェント（omp・claude・codex）を置いたフォルダ。ビルドはパッケージのテスト全体で 1 回。
func Bin(t testing.TB) string {
	t.Helper()
	binOnce.Do(func() {
		root, err := os.MkdirTemp("", "sc-integ-bin-")
		if err != nil {
			t.Fatal(err)
		}
		binRoot = root
		binDir = fakeagenttest.Install(t, root)
	})
	if binDir == "" {
		t.Fatal("擬似エージェントをビルドできていません")
	}
	return binDir
}

// RepoRoot は go.mod のあるフォルダ（テストの作業フォルダから上へたどって探す）。
func RepoRoot(t testing.TB) string {
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
