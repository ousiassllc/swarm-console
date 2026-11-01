package setup

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig は一時フォルダに config.toml を書いてパスを返す。
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const testHome = "/home/tester"

// handwritten は testdata/config/handwritten.toml の内容。
func handwritten(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config", "handwritten.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// answersOf は st の現在値をそのまま答えた Answers（何も変えない入力）。
func answersOf(st *State) Values {
	v := st.Cur.Values
	v.Agents = map[string]Agent{}
	for k, a := range st.Cur.Values.Agents {
		v.Agents[k] = a
	}
	return v
}

// readFile はテストが作った一時ファイルを読む。
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // テストが作った一時ファイル
	if err != nil {
		t.Fatal(err)
	}
	return b
}
