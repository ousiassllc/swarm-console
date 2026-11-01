// Package agenttest はアダプタのテストが testdata/screens のサンプル画面を読む補助を提供する。
package agenttest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Root はテストの作業フォルダから go.mod のあるリポジトリの根を探す（-trimpath でも動くよう実行時の cwd を使う）。
func Root(t testing.TB) string {
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

// Dir は testdata/screens/<name> の絶対パス。
func Dir(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(Root(t), "testdata", "screens", name)
}

// Text はサンプル画面ファイルの中身を返す（注記行を含む）。
func Text(t testing.TB, name, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Dir(t, name), file)) //nolint:gosec // テストの固定のサンプル画面のパス
	if err != nil {
		t.Fatalf("サンプル画面を読めません: %v", err)
	}
	return string(b)
}

// Load はサンプル画面を Screen にして返す。
func Load(t testing.TB, name, file string) agent.Screen {
	t.Helper()
	return agent.NewScreen(agent.SampleLines(Text(t, name, file)))
}

// Files は testdata/screens/<name> の *.txt のファイル名（昇順）。
func Files(t testing.TB, name string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(Dir(t, name), "*.txt"))
	if err != nil || len(m) == 0 {
		t.Fatalf("サンプル画面が見つかりません: %s (%v)", name, err)
	}
	for i := range m {
		m[i] = filepath.Base(m[i])
	}
	return m
}

// New は name のアダプタを上書きなしで作る。
func New(t testing.TB, name string) agent.Adapter {
	t.Helper()
	a, err := agent.New(name, agent.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
