package nativetest

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

// omp アダプタ単体の、tern の画面（TSP）の読み方。会話の記録から組み立てた画面が、普通の画面（実機の PI_TUI_NATIVE=0）と同じ判定・カード・キー列になること。
// fixtures は testdata/native/omp（実機の omp v19.0.1、tern 0.9.29）。会話の記録は rig が書き、omp のプロセスはこのテスト自身（os.Getpid）。

func fixtureText(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agenttest.Root(t), "testdata", "native", "omp", name)) //nolint:gosec // テストの固定の fixture
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ompAdapter は、会話の記録の先頭 n 行を書いた rig の omp アダプタ。
func ompAdapter(t *testing.T, n int) (agent.Adapter, agent.Native) {
	t.Helper()
	ad := newRig(t, n, nil, nil, nil).env.Adapters["omp"]
	nat, ok := ad.(agent.Native)
	if !ok {
		t.Fatal("omp アダプタが agent.Native を実装していない")
	}
	return ad, nat
}

func nativeView(t *testing.T, nat agent.Native) agent.NativeView {
	t.Helper()
	v, ok := nat.NativeScreen(os.Getpid(), 80)
	if !ok {
		t.Fatal("NativeScreen: ok=false")
	}
	return v
}

func TestNativeTitle(t *testing.T) {
	_, nat := ompAdapter(t, 0)
	for title, want := range map[string]bool{
		`tsp;f;{"sf":"s:1","s":9,"ops":[]}`: true,
		"tsp;":                              true,
		"π > s121":                          false,
		"~":                                 false,
		"":                                  false,
		" tsp;f;":                           false,
	} {
		if got := nat.NativeTitle(title); got != want {
			t.Errorf("NativeTitle(%q) = %v, want %v", title, got, want)
		}
	}
}

// 実機の pane_title（tern の画面の omp。tmux が APC の `tsp;f;{...}` をそのままタイトルに取り込んだもの）。
// タイトルには「最後の 1 フレーム」の差分（ops）しか入らないので、タイトルだけから画面全体・状態は作れない（decisions.md P-112）。
func TestRealTernTitlesAreDeltaFrames(t *testing.T) {
	_, nat := ompAdapter(t, 0)
	for _, file := range []string{"title-idle.txt", "title-ask.txt"} {
		title := strings.TrimRight(fixtureText(t, file), "\n")
		if !nat.NativeTitle(title) {
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

// 記録を開いていない omp（まだ何も話していない）は、やり取りの無い idle の omp の画面。
func TestNativeScreenWithoutTranscriptIsIdle(t *testing.T) {
	ad, nat := ompAdapter(t, 0)
	v, ok := nat.NativeScreen(os.Getpid()+100000, 80) // 何も開いていない pid
	if !ok {
		t.Fatal("ok=false")
	}
	if st := ad.Detect(v.Screen).State; st != agent.StateIdle {
		t.Fatalf("State = %s, want idle", st)
	}
	if !ad.MatchScreen(v.Screen) {
		t.Fatal("組み立てた画面が omp の画面と読めない")
	}
}

// 記録を開けない（権限がない）ときは、画面を作れず ok=false（呼び出し側は capture-pane の画面にする）。
func TestNativeScreenUnreadableTranscript(t *testing.T) {
	_, nat := ompAdapter(t, 3)
	path := filepath.Join(os.Getenv("HOME"), ".omp", "agent", "sessions", "--work--", "2026-11-20T13-45-43-697Z_x.jsonl")
	if err := os.Chmod(path, 0); err != nil { //nolint:gosec // テストが作った一時ファイル
		t.Fatal(err)
	}
	if f, err := os.Open(path); err == nil { //nolint:gosec // テストが作った一時ファイル
		_ = f.Close()
		t.Skip("権限を外しても読める環境（root）")
	}
	if _, ok := nat.NativeScreen(os.Getpid(), 80); ok {
		t.Fatal("読めない記録で ok=true")
	}
}

// 同じ質問の、普通の画面（実画面）と組み立てた画面は、状態・カード・キー列が同じ。
func TestNativeAskCardMatchesClassicScreen(t *testing.T) {
	a, nat := ompAdapter(t, 6)
	classic := agent.NewScreen(agent.SampleLines(fixtureText(t, "classic-ask.txt")))
	native := nativeView(t, nat)
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
	sub, ok := a.(agent.Submitter)
	if !ok {
		t.Fatal("omp アダプタが agent.Submitter を実装していない")
	}
	if sub.SubmitFocused(native.Screen) != sub.SubmitFocused(classic) {
		t.Error("SubmitFocused が違う")
	}
}

// pane_tail（Clean）：カードの本文（質問・選択肢）が、普通の画面と同じ行になる。
func TestNativeCleanMatchesClassicCard(t *testing.T) {
	a, nat := ompAdapter(t, 6)
	classic := agent.NewScreen(agent.SampleLines(fixtureText(t, "classic-ask.txt")))
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
	var got []string
	for _, l := range pick(nativeView(t, nat).Screen.Lines) {
		if !strings.Contains(l, "Use the ask tool") && !strings.Contains(l, "▸") {
			got = append(got, l)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Clean\n got=%q\nwant=%q", got, want)
	}
}

func TestNativeCleanKeepsConversationAndDropsChrome(t *testing.T) {
	a, nat := ompAdapter(t, 13)
	text := strings.Join(a.Clean(nativeView(t, nat).Screen.Lines), "\n")
	for _, want := range []string{"Reply with exactly the single word OK", "Blue.", "▸ bash: Sleeping then writing report"} {
		if !strings.Contains(text, want) {
			t.Errorf("pane_tail に %q が無い:\n%s", want, text)
		}
	}
	for _, drop := range []string{"\ue0b6", "tern", "Working"} {
		if strings.Contains(text, drop) {
			t.Errorf("pane_tail に飾り %q が残っている:\n%s", drop, text)
		}
	}
}

func TestNativeDelivered(t *testing.T) {
	_, nat := ompAdapter(t, 10)
	pid := os.Getpid()
	since := time.Date(2026, 11, 20, 14, 3, 50, 0, time.UTC)
	text := "x\n## 条件\n- マージ・Issue の close・デプロイはしない。"
	if !nat.NativeDelivered(pid, text, since) {
		t.Error("記録された指示を見つけられない")
	}
	if nat.NativeDelivered(pid, text, since.Add(5*time.Second)) {
		t.Error("since より前の指示を数えた")
	}
	if nat.NativeDelivered(pid, "ほかの指示です", since) {
		t.Error("違う本文を数えた")
	}
	if nat.NativeDelivered(pid, "  \n", since) {
		t.Error("空の本文を数えた")
	}
	if nat.NativeDelivered(pid+100000, text, since) {
		t.Error("記録を開いていない pid を数えた")
	}
}

// 複数選択・recommended が 2 番目の質問も、普通の画面と同じに読める（チェックボックスの記号・カーソルの位置・Space でチェック）。
func TestNativeCardForMultiAndRecommendedSecond(t *testing.T) {
	ask := func(html string) string {
		args, _ := json.Marshal(map[string]string{"i": "Asking", "html": html})
		return `{"type":"message","id":"q","timestamp":"2026-11-20T14:03:45.000Z","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"toolCall","id":"c1","name":"ask","arguments":` +
			string(args) + `,"intent":"Asking"}]}}`
	}
	cases := []struct {
		name   string
		html   string
		cursor int
		multi  bool
	}{
		{"multi", `<form><fieldset><legend>Toppings</legend><label><input type="checkbox" name="t" value="a"> Cheese</label><label><input type="checkbox" name="t" value="b"> Ham</label></fieldset></form>`, 1, true},
		{"recommended 2 番目", `<form><fieldset><legend>Pick</legend><label><input type="radio" name="p" value="A"> A</label><label><input type="radio" name="p" value="B" checked> B</label><label><input type="radio" name="p" value="C"> C</label></fieldset></form>`, 2, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRig(t, 0, nil, nil, nil)
			g.write(ask(c.html))
			ad := g.env.Adapters["omp"]
			v := nativeView(t, ad.(agent.Native))
			card, ok := ad.ParseCard(v.Screen)
			if !ok || card.Cursor != c.cursor || !card.AllowsCustom {
				t.Fatalf("ParseCard = %+v %v, want cursor %d", card, ok, c.cursor)
			}
			keys := ad.(agent.Toggler).ToggleKeys(v.Screen, 1)
			if c.multi && (len(keys) != 1 || keys[0].Name != "Space") {
				t.Errorf("ToggleKeys = %v, want Space（チェック前の複数選択）", keys)
			}
			if !c.multi && (len(keys) != 0 || !card.Options[1].Recommended) {
				t.Errorf("単一選択: ToggleKeys = %v, options = %+v", keys, card.Options)
			}
		})
	}
}
