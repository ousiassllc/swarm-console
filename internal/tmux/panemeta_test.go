package tmux

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// Issue #121: PaneMeta はペインのシェルの pid と、空白・記号・日本語を含むタイトル（tern の画面の `tsp;f;{…}` を含む）を、そのまま返す。
func TestPaneMeta(t *testing.T) {
	srv, c := setup(t)
	ctx := context.Background()
	id := srv.NewSession("meta", 80, 20, sleepCmd)
	for _, title := range []string{
		"agent: 日本語 の タイトル",
		`tsp;f;{"sf":"s:1","s":9,"ops":[["set","3.context",{"value":0.03}]]}`,
		"",
	} {
		if err := c.SetTitle(ctx, id, title); err != nil {
			t.Fatal(err)
		}
		got, err := c.PaneMeta(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != title || got.PID <= 0 {
			t.Fatalf("PaneMeta = %+v, want title %q と pid > 0", got, title)
		}
	}
	ps, err := c.ListPanes(ctx, []string{"meta"})
	if err != nil || len(ps) != 1 {
		t.Fatalf("%v %+v", err, ps)
	}
	if got, _ := c.PaneMeta(ctx, id); got.PID != ps[0].PID {
		t.Errorf("pid = %d, want list-panes の %d", got.PID, ps[0].PID)
	}
}

func TestPaneMetaRejectsBadAndMissingPane(t *testing.T) {
	_, c := setup(t)
	ctx := context.Background()
	if _, err := c.PaneMeta(ctx, "-t %1"); apperr.As(err).Code != apperr.InvalidArgs {
		t.Errorf("形の悪い ID: %v, want invalid_args", err)
	}
	if _, err := c.PaneMeta(ctx, "%999"); err == nil || !strings.Contains(err.Error(), "tmux") {
		t.Errorf("存在しないペイン: %v, want エラー", err)
	}
}
