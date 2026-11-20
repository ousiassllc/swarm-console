package nativetest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// Issue #121: tern の画面（TSP）の質問カードにはカーソルも Submit への移動も見えない。質問が 2 つ以上のフォームは、
// 選んだあとに次の質問が開くので、確かめずに Enter を重ねると次の質問を選んでしまう。キーを 1 つも送らずに cursor_mismatch で断る。
func TestAskPickRefusesMultiQuestionFormOnTernScreen(t *testing.T) {
	env := panetest.NewEnv()
	ad := panetest.NewNativeAdapter("omp")
	ad.SetQuestions(2)
	ad.SetNativeScreen(101, panetest.ScreenAsk("omp", "どれ？", 1, true, "A", "B"))
	env.Adapters["omp"] = ad
	p := env.Pane("%0", 100, 0, 0, "", []string{"omp"})
	p.Title = panetest.NativeFrame
	env.Tmux.SetPanes(p)
	reg, err := pane.NewRegistry(pane.Deps{Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: env.FS})
	if err != nil {
		t.Fatal(err)
	}
	_, err = run(t, env.Clock, func() (pane.PickResult, error) {
		return ops.New(reg).PickAsk(context.Background(), pane.Ref{ID: "%0", Pos: "T1"}, 2, "")
	})
	if apperr.As(err).Code != apperr.CursorMismatch {
		t.Fatalf("err = %v, want cursor_mismatch", err)
	}
	if got := env.Tmux.OpStrings(); len(got) != 0 {
		t.Fatalf("キーを送った: %v", got)
	}
}
