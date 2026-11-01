package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

// LANG・LC_ALL・LC_CTYPE が無い環境でも、日本語のペインタイトル・cwd が "_" に化けない（Issue #9）。
// tmux は -u が無くロケールが UTF-8 でないと、list-panes の非 ASCII を "_" に置き換える。
func TestListPanesJapaneseWithoutLocale(t *testing.T) {
	for _, k := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(k, "")                        // 終了時の復元を登録する
		if err := os.Unsetenv(k); err != nil { // 空ではなく「未設定」にする（子プロセスは os.Environ() を引き継ぐ）
			t.Fatal(err)
		}
	}
	srv, c := setup(t) // サーバーもこの環境で起動する

	dir := filepath.Join(t.TempDir(), "日本語の作業フォルダ")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(srv.Run("new-session", "-d", "-P", "-F", "#{pane_id}", "-s", "ja", "-x", "80", "-y", "24", "-c", dir, sleepCmd))
	tmuxtest.Eventually(t, id+" の子プロセスが sleep になる", func() bool {
		return strings.TrimSpace(srv.Run("display-message", "-p", "-t", id, "#{pane_current_command}")) == "sleep"
	})

	const title = "日本語タイトル"
	ctx := context.Background()
	if err := c.SetTitle(ctx, id, title); err != nil {
		t.Fatal(err)
	}
	got, err := c.ListPanes(ctx, []string{"ja"})
	if err != nil || len(got) != 1 {
		t.Fatalf("ja のペインは 1 つのはず: %v %+v", err, got)
	}
	if got[0].Title != title || got[0].CurrentPath != dir {
		t.Fatalf("title=%q cwd=%q, want %q %q", got[0].Title, got[0].CurrentPath, title, dir)
	}
}
