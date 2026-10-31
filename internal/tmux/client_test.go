package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/tmux/tmuxtest"
)

// 専用ソケットの実 tmux。既定ソケット（本番）には触れない。

// sleepCmd はペインを生かしておくコマンド（exec しないと sh が残って pane_current_command が "sh" になる）。
const sleepCmd = "exec sleep 300"

func setup(t *testing.T) (*tmuxtest.Server, *Client) {
	t.Helper()
	srv := tmuxtest.New(t)
	return srv, New(srv.Socket)
}

func assertTmuxError(t *testing.T, err error, sub string) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.TmuxError {
		t.Fatalf("tmux_error のはずが %v", err)
	}
	if got := ae.Details["subcommand"]; got != sub || len(ae.Details) != 1 {
		t.Fatalf("details = %v, subcommand=%q だけのはず", ae.Details, sub)
	}
}

func TestListPanesCoordinatesAndFields(t *testing.T) {
	srv, c := setup(t)
	dir := filepath.Join(t.TempDir(), "a\tb dir") // タブ入りのパスでも崩れない
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	id1 := strings.TrimSpace(srv.Run("new-session", "-d", "-P", "-F", "#{pane_id}", "-s", "s1", "-x", "101", "-y", "30", "-c", dir, sleepCmd))
	id2 := strings.TrimSpace(srv.Run("split-window", "-h", "-d", "-P", "-F", "#{pane_id}", "-t", id1, sleepCmd))
	srv.NewSession("s2", 80, 24, sleepCmd)

	ctx := context.Background()
	if err := c.SetTitle(ctx, id1, "agent: 日本語 の タイトル"); err != nil {
		t.Fatal(err)
	}
	got, err := c.ListPanes(ctx, []string{"s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("s1 のペインは 2 つのはず: %+v", got)
	}
	a, b := got[0], got[1]
	if a.ID != id1 || b.ID != id2 {
		t.Fatalf("ID = %s,%s want %s,%s", a.ID, b.ID, id1, id2)
	}
	if a.Session != "s1" || a.Window != 0 || a.PaneIndex != 0 || b.PaneIndex != 1 {
		t.Fatalf("位置が違う: %+v %+v", a, b)
	}
	if a.Top != 0 || a.Left != 0 || b.Top != 0 || b.Left != a.Width+1 || a.Width+1+b.Width != 101 || a.Height != 30 || b.Height != 30 {
		t.Fatalf("座標が違う: %+v %+v", a, b)
	}
	if a.CurrentCmd != "sleep" || a.CurrentPath != dir || a.Title != "agent: 日本語 の タイトル" || a.PID <= 0 || a.Dead {
		t.Fatalf("フィールドが違う: %+v", a)
	}

	both, err := c.ListPanes(ctx, []string{"s1", "s2", "nope"})
	if err != nil || len(both) != 3 {
		t.Fatalf("s1+s2 で 3 ペインのはず: %v %+v", err, both)
	}
	none, err := c.ListPanes(ctx, []string{"nope"})
	if err != nil || len(none) != 0 {
		t.Fatalf("存在しないセッションはエラーにせず空: %v %+v", err, none)
	}
}

func TestListPanesDead(t *testing.T) {
	srv, c := setup(t)
	srv.NewSession("keep", 80, 24, sleepCmd)
	srv.Run("set-option", "-g", "remain-on-exit", "on")
	srv.NewSession("dead", 80, 24, "exit 0")
	tmuxtest.Eventually(t, "ペインが dead になる", func() bool {
		ps, err := c.ListPanes(context.Background(), []string{"dead"})
		return err == nil && len(ps) == 1 && ps[0].Dead
	})
}

func TestNoServer(t *testing.T) {
	srv := tmuxtest.New(t) // サーバーは立てない（セッションを作らない）
	c := New(srv.Socket)
	ctx := context.Background()

	_, err := c.ListPanes(ctx, []string{"0"})
	if !errors.Is(err, ErrNoServer) {
		t.Fatalf("ErrNoServer のはずが %v", err)
	}
	assertTmuxError(t, err, "list-panes")

	if _, err := c.Capture(ctx, "%0", 10); !errors.Is(err, ErrNoServer) {
		t.Fatalf("Capture: ErrNoServer のはずが %v", err)
	}
	if start, err := c.ServerStart(ctx); err != nil || start != "" {
		t.Fatalf("サーバーが無いとき ServerStart は (\"\", nil): %q %v", start, err)
	}
}

func TestTimeout(t *testing.T) {
	srv := tmuxtest.New(t)
	c := New(srv.Socket, WithTimeout(time.Nanosecond))
	_, err := c.Version(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("タイムアウトのはずが %v", err)
	}
	assertTmuxError(t, err, "-V")
}

func TestCapture(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()

	id := srv.NewSession("cap", 80, 20, "printf 'hello\\n日本語\\n'; "+sleepCmd)
	srv.WaitForText(id, "日本語")
	got, err := c.Capture(ctx, id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\n日本語" {
		t.Fatalf("Capture = %q", got)
	}
	if last, _ := c.Capture(ctx, id, 1); last != "日本語" {
		t.Fatalf("lines=1 は最後の 1 行: %q", last)
	}

	// 履歴（スクロールバック）も最後の lines 行に入り、画面下の空行は数えない。
	id = srv.NewSession("hist", 80, 20, "seq 1 100; "+sleepCmd)
	srv.WaitFor(id, func(s string) bool { return strings.Contains(s, "100") })
	got, err = c.Capture(ctx, id, 30)
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(got, "\n")
	if len(ls) != 30 || ls[0] != "71" || ls[29] != "100" || strings.HasSuffix(got, "\n") {
		t.Fatalf("最後の 30 行のはず: %d 行 first=%q last=%q", len(ls), ls[0], ls[len(ls)-1])
	}
	if strings.Contains(got, "\x1b") {
		t.Fatal("ANSI が混じっている")
	}

	// 空の画面は ""。
	id = srv.NewSession("empty", 80, 20, sleepCmd)
	if got, err := c.Capture(ctx, id, 10); err != nil || got != "" {
		t.Fatalf("空の画面は \"\": %q %v", got, err)
	}
}

func TestCaptureRejectsBadArgs(t *testing.T) {
	c := New("sctest-never-started")
	for _, tc := range []struct {
		id    string
		lines int
	}{{"%1;x", 10}, {"-t", 10}, {"", 10}, {"0:0.1", 10}, {"%1", 0}} {
		_, err := c.Capture(context.Background(), tc.id, tc.lines)
		if ae := apperr.As(err); ae.Code != apperr.InvalidArgs {
			t.Errorf("Capture(%q,%d): invalid_args のはずが %v", tc.id, tc.lines, err)
		}
	}
}

func TestSetTitle(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()
	id := srv.NewSession("title", 80, 20, sleepCmd)
	if err := c.SetTitle(ctx, id, "agent: funid #982 日本語"); err != nil {
		t.Fatal(err)
	}
	ps, err := c.ListPanes(ctx, []string{"title"})
	if err != nil || len(ps) != 1 || ps[0].Title != "agent: funid #982 日本語" {
		t.Fatalf("タイトルが反映されていない: %v %+v", err, ps)
	}
	if err := c.SetTitle(ctx, "%999", "x"); err == nil {
		t.Fatal("存在しないペインはエラー")
	} else {
		assertTmuxError(t, err, "select-pane")
	}
}

func TestServerStartAndVersion(t *testing.T) {
	srv, c := setup(t)
	srv.NewSession("v", 80, 20, sleepCmd)
	ctx := context.Background()
	start, err := c.ServerStart(ctx)
	if err != nil || !regexp.MustCompile(`^[0-9]{9,}$`).MatchString(start) {
		t.Fatalf("ServerStart = %q %v", start, err)
	}
	again, _ := c.ServerStart(ctx)
	if again != start {
		t.Fatalf("同じサーバーなら同じ値: %q %q", start, again)
	}
	v, err := c.Version(ctx)
	if err != nil || !strings.HasPrefix(v, "tmux ") {
		t.Fatalf("Version = %q %v", v, err)
	}
	if w := CheckVersion(v); w != "" {
		t.Logf("この環境の tmux は警告対象: %s", w)
	}
}

func TestRunRejectsUnlistedSubcommand(t *testing.T) {
	c := New("sctest-never-started")
	for _, sub := range []string{"kill-server", "kill-pane", "run-shell", "new-session", "split-window", ""} {
		_, err := c.run(context.Background(), sub, "")
		if ae := apperr.As(err); ae.Code != apperr.Internal {
			t.Errorf("%q は実行拒否のはず: %v", sub, err)
		}
	}
}
