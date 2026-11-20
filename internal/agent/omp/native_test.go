package omp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
)

// Issue #121: tern の画面（TSP）の omp は capture-pane に何も描かず、画面の文字では状態を読めない。
// 会話の記録（セッションの jsonl）から組み立てた画面が、普通の画面と同じ判定・カード・キー列になること。
// fixtures（testdata/native/omp）は実機（omp v19.0.1、tern 0.9.29 の tmux 上）で取った記録と、PI_TUI_NATIVE=0 で取った普通の画面。

const nativePID = 4242

// fixtureLines は testdata/native/omp/session.jsonl の行（1 始まりの行番号 = 配列の添字 + 1）。
//
//	1 session / 2-3 「OK」の往復 / 4 ask の指示 / 5 ask の呼び出し / 6 tool_execution_start（質問が出ている）
//	7 ask の結果 / 8 応答「Blue.」 / 9 sleep の指示 / 10 bash の呼び出し / 11 tool_execution_start / 12 bash の結果 / 13 応答（終わり）
func fixtureLines(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agenttest.Root(t), "testdata", "native", "omp", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// nativeRig は pid(nativePID) が、fixture の先頭 n 行を書いたセッションの記録を開いている状態の adapter。
func nativeRig(t *testing.T, n int) *adapter {
	t.Helper()
	g := newSessRig(t)
	g.a = fullAdapter(t, g.a)
	path := filepath.Join(g.dir, "2026-11-20T13-45-43-697Z_x.jsonl")
	g.write(path, fixtureLines(t)[:n]...)
	g.open(nativePID, 3, path)
	g.a.now = func() time.Time { return time.Date(2026, 11, 20, 14, 4, 0, 0, time.UTC) }
	return g.a
}

// fullAdapter は src の置き場の設定を持つ、正規表現まで揃った adapter（New で作る）。
func fullAdapter(t *testing.T, src *adapter) *adapter {
	t.Helper()
	ad, err := New(agent.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	a := ad.(*adapter)
	a.procDir, a.sessionsDir, a.logDir = src.procDir, src.sessionsDir, src.logDir
	return a
}

func nativeScreenOf(t *testing.T, a *adapter) agent.NativeView {
	t.Helper()
	v, ok := a.NativeScreen(nativePID, 80)
	if !ok {
		t.Fatal("NativeScreen: ok=false")
	}
	return v
}

// 記録の何行目まで書かれたかで、状態・活動が決まる。起動しただけ（記録なし）は idle。
func TestNativeStateFollowsTranscript(t *testing.T) {
	cases := []struct {
		name     string
		lines    int
		state    agent.State
		activity string
		elapsed  string
	}{
		{"最初の往復が終わった", 3, agent.StateIdle, "", ""},
		{"質問が出ている", 6, agent.StateAsking, "Asking colour choice", ""},
		{"質問の呼び出しだけ（tool_execution_start の前）", 5, agent.StateAsking, "Asking colour choice", ""},
		{"回答が記録され、モデルの応答を待つ", 7, agent.StateWorking, "Asking colour choice", "16m"},
		{"応答が終わった", 8, agent.StateIdle, "", ""},
		{"指示を受けた", 9, agent.StateWorking, "Working…", "8s"},
		{"長いコマンドの実行中", 11, agent.StateWorking, "Sleeping then writing report", "8s"},
		{"コマンドの結果をモデルが読む", 12, agent.StateWorking, "Sleeping then writing report", "8s"},
		{"終わった", 13, agent.StateIdle, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := nativeRig(t, c.lines)
			got := a.Detect(nativeScreenOf(t, a).Screen)
			if got.State != c.state || got.Activity != c.activity {
				t.Errorf("Detect = %+v, want state=%s activity=%q", got, c.state, c.activity)
			}
			if c.state == agent.StateWorking && !strings.HasPrefix(got.Elapsed, c.elapsed) {
				t.Errorf("Elapsed = %q, want prefix %q", got.Elapsed, c.elapsed)
			}
		})
	}
}

// 作業中の StableText は経過時間で変わらない（画面が止まったかの判定が、秒の更新で毎回変わらない）。
func TestNativeStableTextIgnoresElapsed(t *testing.T) {
	a := nativeRig(t, 11)
	first := a.StableText(nativeScreenOf(t, a).Screen.Lines)
	a.now = func() time.Time { return time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC) }
	if second := a.StableText(nativeScreenOf(t, a).Screen.Lines); first != second {
		t.Errorf("StableText が経過時間で変わる:\n%q\n%q", first, second)
	}
}
