package omp

import (
	"encoding/json"
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

func TestNativeTitle(t *testing.T) {
	a := &adapter{}
	for title, want := range map[string]bool{
		`tsp;f;{"sf":"s:1","s":9,"ops":[]}`: true,
		"tsp;":                              true,
		"π > s121":                          false,
		"~":                                 false,
		"":                                  false,
		" tsp;f;":                           false,
	} {
		if got := a.NativeTitle(title); got != want {
			t.Errorf("NativeTitle(%q) = %v, want %v", title, got, want)
		}
	}
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

func TestNativeScreenWithoutTranscriptIsIdle(t *testing.T) {
	g := newSessRig(t) // pid は何も開いていない
	g.a = fullAdapter(t, g.a)
	v, ok := g.a.NativeScreen(nativePID, 80)
	if !ok {
		t.Fatal("ok=false")
	}
	if st := g.a.Detect(v.Screen).State; st != agent.StateIdle {
		t.Fatalf("State = %s, want idle", st)
	}
	if !g.a.MatchScreen(v.Screen) {
		t.Fatal("組み立てた画面が omp の画面と読めない")
	}
}

// 記録を開けない（権限がない）ときは、画面を作れず ok=false（呼び出し側は capture-pane の画面にする）。
func TestNativeScreenUnreadableTranscript(t *testing.T) {
	g := newSessRig(t)
	g.a = fullAdapter(t, g.a)
	path := filepath.Join(g.dir, "gone.jsonl")
	g.write(path, "{}")
	g.open(nativePID, 3, path)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(path); err == nil { //nolint:gosec // テストが作った一時ファイル
		_ = f.Close()
		t.Skip("権限を外しても読める環境（root）")
	}
	if _, ok := g.a.NativeScreen(nativePID, 80); ok {
		t.Fatal("読めない記録で ok=true")
	}
}

// 同じ質問の、普通の画面（実画面）と組み立てた画面は、状態・カード・キー列が同じ。
func TestNativeAskCardMatchesClassicScreen(t *testing.T) {
	a := nativeRig(t, 6)
	classic := agent.NewScreen(agent.SampleLines(readFixture(t, "classic-ask.txt")))
	native := nativeScreenOf(t, a)
	if native.AskQuestions != 1 {
		t.Errorf("AskQuestions = %d, want 1", native.AskQuestions)
	}
	if c, n := a.Detect(classic).State, a.Detect(native.Screen).State; c != agent.StateAsking || n != c {
		t.Fatalf("State: classic=%s native=%s", c, n)
	}
	cc, cok := a.ParseCard(classic)
	nc, nok := a.ParseCard(native.Screen)
	if !cok || !nok || cc.Question != nc.Question || cc.Cursor != nc.Cursor || cc.AllowsCustom != nc.AllowsCustom || len(cc.Options) != len(nc.Options) {
		t.Fatalf("ParseCard\nclassic=%+v (%v)\nnative =%+v (%v)", cc, cok, nc, nok)
	}
	for i := range cc.Options {
		if cc.Options[i] != nc.Options[i] {
			t.Errorf("Options[%d]: classic=%+v native=%+v", i, cc.Options[i], nc.Options[i])
		}
	}
	for n := 1; n <= len(cc.Options); n++ {
		ck, cerr := a.PickKeys(cc, n)
		nk, nerr := a.PickKeys(nc, n)
		if cerr != nil || nerr != nil || len(ck) != len(nk) {
			t.Errorf("PickKeys(%d): classic=%v/%v native=%v/%v", n, ck, cerr, nk, nerr)
		}
	}
	if a.SubmitFocused(native.Screen) != a.SubmitFocused(classic) {
		t.Error("SubmitFocused が違う")
	}
}

// pane_tail（Clean）：カードの本文（質問・選択肢）が、普通の画面と同じ行になる。
func TestNativeCleanMatchesClassicCard(t *testing.T) {
	a := nativeRig(t, 6)
	classic := agent.NewScreen(agent.SampleLines(readFixture(t, "classic-ask.txt")))
	pick := func(lines []string) []string {
		var out []string
		for _, l := range a.Clean(lines) {
			if strings.Contains(l, "Pick a colour") || strings.Contains(l, "Red (recommended)") || strings.Contains(l, "Blue") || strings.Contains(l, "Other:") {
				out = append(out, strings.TrimSpace(l))
			}
		}
		return out
	}
	// 普通の画面は指示の行（ユーザーの発言）にも質問の語が出る。カードの行だけを比べる。
	want := pick(classic.Lines[len(classic.Lines)-12:])
	got := pick(nativeScreenOf(t, a).Screen.Lines)
	var gotCard []string
	for _, l := range got {
		if !strings.Contains(l, "Use the ask tool") && !strings.Contains(l, "▸") {
			gotCard = append(gotCard, l)
		}
	}
	if strings.Join(gotCard, "\n") != strings.Join(want, "\n") {
		t.Errorf("Clean\n got=%q\nwant=%q", gotCard, want)
	}
}

func TestNativeCleanKeepsConversationAndDropsChrome(t *testing.T) {
	a := nativeRig(t, 13)
	text := strings.Join(a.Clean(nativeScreenOf(t, a).Screen.Lines), "\n")
	for _, want := range []string{"Reply with exactly the single word OK", "Blue.", "▸ bash: Sleeping then writing report"} {
		if !strings.Contains(text, want) {
			t.Errorf("pane_tail に %q が無い:\n%s", want, text)
		}
	}
	for _, drop := range []string{string(statusCap), "tern", "Working"} {
		if strings.Contains(text, drop) {
			t.Errorf("pane_tail に飾り %q が残っている:\n%s", drop, text)
		}
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

func TestNativeDelivered(t *testing.T) {
	a := nativeRig(t, 10)
	since := time.Date(2026, 11, 20, 14, 3, 50, 0, time.UTC)
	text := "x\n## 条件\n- マージ・Issue の close・デプロイはしない。"
	if !a.NativeDelivered(nativePID, text, since) {
		t.Error("記録された指示を見つけられない")
	}
	if a.NativeDelivered(nativePID, text, since.Add(5*time.Second)) {
		t.Error("since より前の指示を数えた")
	}
	if a.NativeDelivered(nativePID, "ほかの指示です", since) {
		t.Error("違う本文を数えた")
	}
	if a.NativeDelivered(nativePID, "  \n", since) {
		t.Error("空の本文を数えた")
	}
	if a.NativeDelivered(99999, text, since) {
		t.Error("記録を開いていない pid を数えた")
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agenttest.Root(t), "testdata", "native", "omp", name)) //nolint:gosec // テストの固定の fixture
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 実機の pane_title（tern の画面の omp。tmux が APC の `tsp;f;{...}` をそのままタイトルに取り込んだもの）。
// タイトルには「最後の 1 フレーム」の差分（ops）しか入らないので、タイトルだけから画面全体・状態は作れない（decisions.md P-109）。
func TestRealTernTitlesAreDeltaFrames(t *testing.T) {
	a := &adapter{}
	for _, file := range []string{"title-idle.txt", "title-ask.txt"} {
		title := strings.TrimRight(readFixture(t, file), "\n")
		if !a.NativeTitle(title) {
			t.Errorf("%s: 実機のタイトルを tern の画面と読めない", file)
		}
		var frame struct {
			SF  string            `json:"sf"`
			S   int               `json:"s"`
			Ops []json.RawMessage `json:"ops"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(title, "tsp;f;")), &frame); err != nil {
			t.Fatalf("%s: フレームの JSON を読めない: %v", file, err)
		}
		if frame.SF == "" || frame.S < 2 || len(frame.Ops) == 0 {
			t.Errorf("%s: sf=%q s=%d ops=%d", file, frame.SF, frame.S, len(frame.Ops))
		}
		if strings.Contains(title, `"id":"main"`) {
			t.Errorf("%s: 画面全体の追加（main）が入っている。差分ではない", file)
		}
	}
}
