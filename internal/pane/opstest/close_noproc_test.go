package opstest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// /proc が読めない（プロセス木が取れない）環境で、終了後も画面にエージェントの表示が残っていても、
// 前面コマンドが shell に戻れば正常終了とみなす。exit_timeout にしない（Issue #35）。
func TestCloseWithoutProcTreeIgnoresLeftoverScreen(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	agentPane := f.env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), nil)
	agentPane.CurrentCmd = "omp" // 前面はエージェント
	shellPane := agentPane
	shellPane.CurrentCmd = "bash"
	f.env.Tmux.SetPanes(agentPane)
	f.env.Host.ClearTree(100) // /proc が読めない

	var exited, returned bool
	var last string
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "keys" && o.Keys[0] == "Enter":
			switch last {
			case "/exit":
				exited = true
			case "cd ~", "clear":
				f.env.Tmux.SetScreen("%0", "~ $ ") // cd ~ 以降で画面は片付く
			}
		}
	})
	f.env.Tmux.OnList(func() {
		if exited && !returned {
			returned = true
			// 前面は shell に戻ったが、画面にはエージェントの表示が残ったまま（スナップショットの画面は idle に見える）。
			f.env.Tmux.SetPanes(shellPane)
		}
	})

	res, err := run(t, f.env.Clock, func() (pane.CloseResult, error) { return f.op.Close(context.Background(), ref) })
	if err != nil {
		t.Fatalf("正常終了が失敗した: %v", err)
	}
	if !res.OK || res.State != agent.StateShell {
		t.Fatalf("res = %+v", res)
	}
	if want := []string{`literal %0 "/exit"`, "keys %0 Enter", `literal %0 "cd ~"`, "keys %0 Enter", `literal %0 "clear"`, "keys %0 Enter", `title %0 ""`}; !equalStrings(opStrings(f), want) {
		t.Fatalf("ops = %v", opStrings(f))
	}
}
