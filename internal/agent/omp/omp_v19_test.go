package omp_test

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// omp v19 の質問フォーム（Issue #111）の実画面。枠に区切り線（├）もフッターもなく、枠のあとに活動行・ステータス行・入力欄の下枠が続く。
// 画面の最後の行が枠の下枠ではなくなったので、v18 までの「最後の行が `╰──`」では asking にならなかった。

const (
	sdkQuestion  = "SDK distribution decision: how to treat the acceptance criterion"
	sdkRecommend = "Recommended: keep CI artifacts only and file a separate issue for registry publish, and mention it as unmet in the report so that humans can follow up later on"
	sdkAttach    = "Attach artifacts to GitHub Releases manually"
)

func sdk(cursor int) *agent.Card {
	return &agent.Card{
		Question:     sdkQuestion,
		Options:      []agent.Option{{N: 1, Label: sdkRecommend}, {N: 2, Label: sdkAttach}},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

// b1SDK は 2026-11-19 の B1 で asking にならなかった質問（ask-v19-b1.txt）。
func b1SDK(cursor int) *agent.Card {
	return &agent.Card{
		Question: "受け入れ条件「SDK を配布できる」の扱い",
		Options: []agent.Option{
			{N: 1, Label: "推奨: 生成物の CI 成果物保存（`funid-sdk-mobile`）までを「配布できる」とし、レジストリへの publish は別 Issue にする。報告に未達の点として書く"},
			{N: 2, Label: "GitHub Releases に成果物を添付する手順を足す（タグ作成は人が行う。このバッチではタグ・リリースを作らない）"},
		},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

func v19Toppings(cursor int) *agent.Card {
	return &agent.Card{
		Question:     "Pick toppings",
		Options:      []agent.Option{{N: 1, Label: "Cheese"}, {N: 2, Label: "Ham"}, {N: 3, Label: "Olives"}},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

func v19Size(cursor int) *agent.Card {
	return &agent.Card{
		Question:     "Pick a size",
		Options:      []agent.Option{{N: 1, Label: "Small"}, {N: 2, Label: "Large"}},
		Cursor:       cursor,
		AllowsCustom: true,
	}
}

var v19Rows = []agenttest.Row{
	{File: "ask-v19-b1.txt", State: agent.StateAsking, Activity: "Asking SDK distribution decision", Card: b1SDK(1)},
	{File: "ask-v19-1.txt", State: agent.StateAsking, Activity: "Asking SDK distribution decision", Card: sdk(1)},
	{File: "ask-v19-2.txt", State: agent.StateAsking, Activity: "Asking SDK distribution decision", Card: sdk(2)},
	{File: "ask-v19-other.txt", State: agent.StateAsking, Activity: "Asking SDK distribution decision", Card: sdk(3)},
	// カーソルが Submit の行にあると質問は 1 行にたたまれ、選択肢の行が見えない。asking のままだがカードは読めない。
	{File: "ask-v19-submit.txt", State: agent.StateAsking, Activity: "Asking SDK distribution decision"},
	{File: "ask-v19-multi-1.txt", State: agent.StateAsking, Activity: "Asking toppings", Card: v19Toppings(1)},
	{File: "ask-v19-multi-toggled.txt", State: agent.StateAsking, Activity: "Asking toppings", Card: v19Toppings(2)},
	{File: "ask-v19-multi-submit.txt", State: agent.StateAsking, Activity: "Asking toppings"},
	// 質問が 2 つのフォームは、開いている質問だけを読む。
	{File: "ask-v19-form-1.txt", State: agent.StateAsking, Activity: "Asking toppings and size", Card: v19Toppings(1)},
	{File: "ask-v19-form-1-checked.txt", State: agent.StateAsking, Activity: "Asking toppings and size", Card: v19Toppings(1)},
	{File: "ask-v19-form-2.txt", State: agent.StateAsking, Activity: "Asking toppings and size", Card: v19Size(1)},
	{File: "ask-v19-form-typed.txt", State: agent.StateAsking, Activity: "Asking toppings and size", Card: v19Size(3)},
	// Esc で入力欄へフォーカスが移った状態。質問は開いたままで、カーソルの印だけがない。
	{File: "ask-v19-unfocused.txt", State: agent.StateAsking, Activity: "Asking fruit choice", Card: &agent.Card{
		Question: "Pick a fruit", Options: []agent.Option{{N: 1, Label: "Apple"}, {N: 2, Label: "Banana"}}, AllowsCustom: true}},
	// 回答後に残る結果の枠（`╭─── Ask`）・キャンセル後は asking にしない。
	{File: "idle-after-ask-v19.txt", State: agent.StateIdle},
	{File: "cancelled-idle-v19.txt", State: agent.StateIdle},
}

func TestKeysV19(t *testing.T) {
	agenttest.CheckKeys(t, agenttest.New(t, "omp"), "omp", []agenttest.KeyRow{
		{File: "ask-v19-b1.txt", N: 1, Want: []string{}},
		{File: "ask-v19-b1.txt", N: 2, Want: []string{"Down"}},
		{File: "ask-v19-b1.txt", N: 0, Want: []string{"Down", "Down"}},
		{File: "ask-v19-2.txt", N: 1, Want: []string{"Up"}},
		{File: "ask-v19-other.txt", N: 1, Want: []string{"Up", "Up"}},
		{File: "ask-v19-other.txt", N: 0, Want: []string{}},
		{File: "ask-v19-other.txt", N: 3, ErrCode: apperr.OptionOutOfRange},
		{File: "ask-v19-multi-1.txt", N: 3, Want: []string{"Down", "Down"}},
		{File: "ask-v19-multi-toggled.txt", N: 1, Want: []string{"Up"}},
		{File: "ask-v19-multi-1.txt", N: 0, Want: []string{"Down", "Down", "Down"}},
		{File: "ask-v19-form-2.txt", N: 2, Want: []string{"Down"}},
		{File: "ask-v19-form-typed.txt", N: 1, Want: []string{"Up", "Up"}},
		// カーソルの印がない（入力欄へフォーカスが移った）カードは、動かし方が決まらない。
		{File: "ask-v19-unfocused.txt", N: 1, ErrCode: apperr.CursorMismatch},
	})
}

// カーソルが Submit の行にある画面だけ SubmitFocused が真になる。Submit の Enter を送る目印で、v18 の画面や結果の枠では読まない。
func TestSubmitFocused(t *testing.T) {
	sub, ok := agenttest.New(t, "omp").(agent.Submitter)
	if !ok {
		t.Fatal("omp のアダプタが agent.Submitter を実装していません")
	}
	for file, want := range map[string]bool{
		"ask-v19-submit.txt":       true,
		"ask-v19-multi-submit.txt": true,
		"ask-v19-1.txt":            false,
		"ask-v19-other.txt":        false,
		"ask-v19-form-1.txt":       false,
		"ask-v19-form-typed.txt":   false,
		"ask-multi.txt":            false,
		"ask-1.txt":                false,
		"custom-input.txt":         false,
		"idle-after-ask-v19.txt":   false,
		"idle.txt":                 false,
	} {
		if got := sub.SubmitFocused(agenttest.Load(t, "omp", file)); got != want {
			t.Errorf("%s: SubmitFocused = %v, want %v", file, got, want)
		}
	}
}

// 質問フォームの画面は、ステータス行が枠のあとにあっても omp の画面と判別でき、pane_tail の Clean は質問の本文を残す。
func TestMatchAndCleanV19(t *testing.T) {
	a := agenttest.New(t, "omp")
	if !a.MatchScreen(agenttest.Load(t, "omp", "ask-v19-1.txt")) {
		t.Error("v19 の質問フォームを omp の画面と判別できません")
	}
	drop := []string{"\ue0b6", "╭", "╰", "Asking SDK distribution decision"}
	agenttest.CheckCleanHas(t, a, "omp", "ask-v19-1.txt", []string{sdkQuestion, sdkAttach, "Other: type your own"}, drop)
}

// 複数選択は、カーソルがある行がチェックされていないチェックボックスのときだけ Space を返す。単一選択（丸）・チェック済み・読めない画面では空。
func TestToggleKeys(t *testing.T) {
	tg, ok := agenttest.New(t, "omp").(agent.Toggler)
	if !ok {
		t.Fatal("omp のアダプタが agent.Toggler を実装していません")
	}
	for _, tc := range []struct {
		file string
		n    int
		want int
	}{
		{"ask-v19-multi-1.txt", 1, 1},
		{"ask-v19-multi-1.txt", 3, 1},
		{"ask-v19-multi-toggled.txt", 1, 0}, // 1・2 番はチェック済み
		{"ask-v19-multi-toggled.txt", 2, 0},
		{"ask-v19-multi-toggled.txt", 3, 1},
		{"ask-v19-form-1.txt", 1, 1},
		{"ask-multi.txt", 1, 1}, // v18 の複数選択
		{"ask-multi-toggled.txt", 1, 0},
		{"ask-v19-1.txt", 1, 0}, // 単一選択（丸）
		{"ask-1.txt", 2, 0},
		{"ask-v19-multi-submit.txt", 1, 0}, // 選択肢の行が見えない
		{"ask-v19-multi-1.txt", 9, 0},
	} {
		keys := tg.ToggleKeys(agenttest.Load(t, "omp", tc.file), tc.n)
		if len(keys) != tc.want || (tc.want == 1 && keys[0].Name != "Space") {
			t.Errorf("%s n=%d: keys = %v, want %d 個", tc.file, tc.n, keys, tc.want)
		}
	}
}
