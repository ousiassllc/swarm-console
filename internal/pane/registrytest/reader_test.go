package registrytest

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

func TestTail(t *testing.T) {
	env := panetest.NewEnv()
	p := env.Pane("%0", 100, 0, 0, "l1\nl2 STATUSLINE\nl3\nl4\n\n\n", []string{"omp"})
	sh := env.Pane("%1", 101, 0, 50, "a STATUSLINE\nb\n", nil)
	env.Tmux.SetPanes(p, sh)
	r := newTestRegistry(t, env)
	ctx := context.Background()

	got, err := r.Tail(ctx, ref("%0", "T1"), 3, false)
	if err != nil || got.Text != "l1\nl3\nl4" || got.Truncated {
		t.Fatalf("clean: %+v %v", got, err)
	}
	got, _ = r.Tail(ctx, ref("%0", "T1"), 3, true)
	if got.Text != "l2 STATUSLINE\nl3\nl4" {
		t.Fatalf("raw: %+v", got)
	}
	// エージェントなしは Clean しない。
	got, _ = r.Tail(ctx, ref("%1", "T2"), 5, false)
	if got.Text != "a STATUSLINE\nb" {
		t.Fatalf("エージェントなし: %+v", got)
	}
	// 1 行 500 字。
	env.Tmux.SetScreen("%0", strings.Repeat("あ", 600)+"\nok\n")
	got, _ = r.Tail(ctx, ref("%0", "T1"), 5, true)
	if first, _, _ := strings.Cut(got.Text, "\n"); len([]rune(first)) != 500 || !got.Truncated {
		t.Fatalf("1 行の上限: %d %v", len([]rune(first)), got.Truncated)
	}
	// 全体 20,000 字（末尾側を残す）。
	env.Tmux.SetScreen("%0", strings.Repeat(strings.Repeat("x", 99)+"\n", 250)+"END\n")
	got, _ = r.Tail(ctx, ref("%0", "T1"), 250, true)
	if n := len([]rune(got.Text)); n != 20000 || !got.Truncated || !strings.HasSuffix(got.Text, "END") {
		t.Fatalf("全体の上限: %d %v", n, got.Truncated)
	}
	if _, err := r.Tail(ctx, ref("%99", "X"), 5, false); apperr.As(err).Code != apperr.PaneNotFound {
		t.Fatalf("消えたペイン: %v", err)
	}
	env.Tmux.SetCaptureErr("%0", errBoom)
	if _, err := r.Tail(ctx, ref("%0", "T1"), 5, true); apperr.As(err).Code != apperr.TmuxError {
		t.Fatalf("capture 失敗: %v", err)
	}
}

func TestCard(t *testing.T) {
	env := panetest.NewEnv()
	ask := env.Pane("%0", 100, 0, 0, panetest.ScreenAsk("omp", "どれ？", 2, true, "A", "B"), []string{"omp"})
	idle := env.Pane("%1", 101, 0, 50, panetest.ScreenIdle("omp"), []string{"omp"})
	sh := env.Pane("%2", 102, 20, 0, panetest.ScreenShell(), nil)
	env.Tmux.SetPanes(ask, idle, sh)
	r := newTestRegistry(t, env)
	ctx := context.Background()

	c, err := r.Card(ctx, ref("%0", "T1"))
	if err != nil || c == nil || c.Question != "どれ？" || len(c.Options) != 2 || c.Cursor != 2 || !c.AllowsCustom || c.Truncated {
		t.Fatalf("card = %+v, %v", c, err)
	}
	if c, err := r.Card(ctx, ref("%1", "T2")); c != nil || err != nil {
		t.Fatalf("asking でなければ (nil, nil): %+v %v", c, err)
	}
	if _, err := r.Card(ctx, ref("%2", "B1")); apperr.As(err).Code != apperr.UnsupportedAgent {
		t.Fatalf("エージェントなし: %v", err)
	}
	// 上限: 選択肢 20・質問 1000 字・label 300 字。
	labels := make([]string, 25)
	for i := range labels {
		labels[i] = "L" + strings.Repeat("あ", 400)
	}
	env.Tmux.SetScreen("%0", panetest.ScreenAsk("omp", strings.Repeat("問", 1200), 1, false, labels...))
	c, _ = r.Card(ctx, ref("%0", "T1"))
	if c == nil || len(c.Options) != 20 || len([]rune(c.Question)) != 1000 || len([]rune(c.Options[0].Label)) != 300 || !c.Truncated {
		t.Fatalf("上限: %+v", c)
	}
}
