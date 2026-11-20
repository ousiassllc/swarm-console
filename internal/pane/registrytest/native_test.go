package registrytest

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// Issue #121: tern の画面（TSP）のペイン。画面は capture-pane ではなく、アダプタが会話の記録から組み立てる。
// ここはフェイクのアダプタで、どの条件のときに組み立てた画面を使い、使わないかを確かめる（本物の omp アダプタの読みは nativetest）。

// nativeEnv は %0（シェル pid 100・omp は 101）を tern の画面にする。capture-pane は空、タイトルは TSP のフレーム。
func nativeEnv(t *testing.T) (*panetest.Env, *panetest.NativeAdapter) {
	t.Helper()
	env := panetest.NewEnv()
	ad := panetest.NewNativeAdapter("omp")
	env.Adapters["omp"] = ad
	p := env.Pane("%0", 100, 0, 0, "", []string{"omp"})
	p.Title = panetest.NativeFrame
	env.Tmux.SetPanes(p)
	return env, ad
}

const omppid = 101 // FakeHost は shell の pid + 1 を omp の pid にする

func TestNativePaneIsReadFromTheComposedScreen(t *testing.T) {
	env, ad := nativeEnv(t)
	ad.SetNativeScreen(omppid, panetest.ScreenWorking("omp", "1", "調査"))
	r := newTestRegistry(t, env)
	p := byID(t, mustRefresh(t, r), "%0")
	if p.State != agent.StateWorking || p.Activity != "調査" || p.Agent != "omp" {
		t.Fatalf("state=%s activity=%q agent=%q", p.State, p.Activity, p.Agent)
	}
	if p.Title != "" {
		t.Errorf("title = %q, want 空（TSP のフレームを出さない）", p.Title)
	}
	tail, err := r.Tail(context.Background(), ref("%0", "T1"), 5, true)
	if err != nil || !strings.Contains(tail.Text, "調査") {
		t.Fatalf("tail = %+v %v", tail, err)
	}
	ad.SetNativeScreen(omppid, panetest.ScreenAsk("omp", "どれ？", 1, true, "A", "B"))
	card, err := r.Card(context.Background(), ref("%0", "T1"))
	if err != nil || card == nil || card.Question != "どれ？" || len(card.Options) != 2 {
		t.Fatalf("card = %+v %v", card, err)
	}
}

// タイトルが TSP でないペインは、アダプタが native でも capture-pane の画面を使う（普通の画面の動きは変わらない）。
func TestClassicTitleKeepsCaptureScreen(t *testing.T) {
	env, ad := nativeEnv(t)
	ad.SetNativeScreen(omppid, panetest.ScreenWorking("omp", "1", "組み立てた画面"))
	p := env.Pane("%0", 100, 0, 0, panetest.ScreenIdle("omp"), []string{"omp"})
	p.Title = "π > work"
	env.Tmux.SetPanes(p)
	r := newTestRegistry(t, env)
	got := byID(t, mustRefresh(t, r), "%0")
	if got.State != agent.StateIdle || got.Title != "π > work" {
		t.Fatalf("state=%s title=%q, want idle / そのまま", got.State, got.Title)
	}
}

// Native でないアダプタ（claude など）は、タイトルが TSP でも capture-pane の画面を使う。
func TestNonNativeAdapterIgnoresTspTitle(t *testing.T) {
	env := panetest.NewEnv()
	p := env.Pane("%0", 100, 0, 0, panetest.ScreenWorking("claude", "1", "作業"), []string{"claude"})
	p.Title = panetest.NativeFrame
	env.Tmux.SetPanes(p)
	got := byID(t, mustRefresh(t, newTestRegistry(t, env)), "%0")
	if got.State != agent.StateWorking || got.Title != panetest.NativeFrame {
		t.Fatalf("state=%s title=%q", got.State, got.Title)
	}
}

// 組み立てられない（記録が読めない）ときは capture-pane の画面に戻る。画面が空なら idle のまま（今までの動き）。
func TestNativeFallsBackToCaptureWhenComposeFails(t *testing.T) {
	env, _ := nativeEnv(t) // SetNativeScreen していない = ok=false
	r := newTestRegistry(t, env)
	got := byID(t, mustRefresh(t, r), "%0")
	if got.State != agent.StateIdle || got.Title != "" {
		t.Fatalf("state=%s title=%q, want idle / 空（TSP のタイトルは出さない）", got.State, got.Title)
	}
}

// omp が終了してシェルに戻ったペインは、古い TSP のタイトルが残っていても shell。
func TestShellWithStaleTspTitleIsShell(t *testing.T) {
	env := panetest.NewEnv()
	env.Adapters["omp"] = panetest.NewNativeAdapter("omp")
	p := env.Pane("%0", 100, 0, 0, panetest.ScreenShell(), nil)
	p.Title = panetest.NativeFrame
	env.Tmux.SetPanes(p)
	got := byID(t, mustRefresh(t, newTestRegistry(t, env)), "%0")
	if got.State != agent.StateShell || got.Agent != "" || got.Title != "" {
		t.Fatalf("state=%s agent=%q title=%q, want shell / 空のタイトル", got.State, got.Agent, got.Title)
	}
}
