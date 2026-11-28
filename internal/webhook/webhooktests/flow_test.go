package webhooktests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/testutil"
)

// 1 つのペイン（セッション 0・T2・%3）の状態を 1 つずつ渡して、送信が終わるまで待つ補助。
// 「そのあと何も届かないこと」を確かめるので、固定時間の待ちではなく、台帳の outbox が空になる（送る件が無くなる）のを待つ。

// paneIn は session のペイン（%3・T2）。
func paneIn(session string, state agent.State) pane.PaneState {
	return pane.PaneState{
		ID: "%3", Session: session, Pos: "T2", Local: "T2", GlobalPos: session + ":T2", State: state,
		Note: &notes.Summary{From: "そら", Repo: "ousiassllc/swarm-console", Items: []string{"#93"}, Next: "PR を出す"},
	}
}

// observe は 5 秒進めて panes のスナップショットを渡し、始まった送信が終わるまで待つ。serverStart は tmux の起動時刻（空なら "100"）。
func (g *rig) observe(serverStart string, panes ...pane.PaneState) {
	g.t.Helper()
	if serverStart == "" {
		serverStart = "100"
	}
	g.at = g.at.Add(5 * time.Second)
	g.n.Observe(&pane.Snapshot{At: g.at, Panes: panes, ServerStart: serverStart})
	g.settle()
}

// see は session のペインを state にして渡す。
func (g *rig) see(session string, state agent.State) {
	g.t.Helper()
	g.observe("", paneIn(session, state))
}

// settle は進行中の送信が終わるまで（台帳の outbox が空になるまで）待つ。上限つき。Observe は送る件を台帳に書いてから戻るので、
// 件を作ったなら、戻った直後の台帳の outbox は空でない。
func (g *rig) settle() {
	g.t.Helper()
	var last string
	defer func() {
		if g.t.Failed() {
			g.t.Logf("送信が終わりません:\n%s", last)
		}
	}()
	testutil.Eventually(g.t, "進行中の送信が終わる（台帳の outbox が空になる）", func() bool {
		last = g.ledger()
		return last == "" || strings.Contains(last, `"outbox":{}`)
	})
}

// report は session の最終報告ファイルを書く。body の長さが変わると「更新された」と見える。
func (g *rig) report(session, body string) {
	g.t.Helper()
	dir := filepath.Join(g.dir, "reports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, session+"_T2.md"), []byte(body), 0o600); err != nil {
		g.t.Fatal(err)
	}
}

// count は r に届いた POST のうち、event が ev のものの数。
func count(t *testing.T, r *receiver, ev string) int {
	t.Helper()
	n := 0
	for _, h := range r.received() {
		if payloadOf(t, h).Event == ev {
			n++
		}
	}
	return n
}
