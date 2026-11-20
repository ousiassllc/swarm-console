package panetest

import (
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// NativeTitle は NativeAdapter が tern の画面（TSP）のペインとみなすタイトルの接頭辞。
const NativeTitle = "tsp;"

// NativeFrame は tern の画面のペインのタイトル（最後の 1 フレーム）の例。
const NativeFrame = NativeTitle + `f;{"sf":"s:1","s":9,"ops":[["set","3.context",{"value":0.03}]]}`

// NativeAdapter は FakeAdapter に agent.Native を足したもの（Issue #121）。tern の画面のペインは capture-pane が空で、
// 画面は SetNativeScreen で与えた「会話の記録から組み立てた画面」になる（FakeAdapter の画面の約束ごとで書く）。
type NativeAdapter struct {
	FakeAdapter
	mu        sync.Mutex
	screens   map[int]string
	delivered map[int][]delivery
	questions int // 質問カードが出ているときのフォームの質問の数（0 なら 1）
}

type delivery struct {
	text string
	at   time.Time
}

var (
	_ agent.Adapter = (*NativeAdapter)(nil)
	_ agent.Native  = (*NativeAdapter)(nil)
)

// NewNativeAdapter は画面がまだ無い NativeAdapter を作る。
func NewNativeAdapter(name string) *NativeAdapter {
	return &NativeAdapter{FakeAdapter: FakeAdapter{name}, screens: map[int]string{}, delivered: map[int][]delivery{}}
}

// SetNativeScreen は pid の会話の記録から組み立てた画面を差し替える。
func (a *NativeAdapter) SetNativeScreen(pid int, screen string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.screens[pid] = screen
}

// SetQuestions は質問カードが出ているときのフォーム全体の質問の数を決める（既定は 1）。
func (a *NativeAdapter) SetQuestions(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.questions = n
}

// AddDelivered は pid の会話の記録に、at に text が指示として書かれたことにする。
func (a *NativeAdapter) AddDelivered(pid int, text string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delivered[pid] = append(a.delivered[pid], delivery{text, at})
}

// NativeTitle は title が NativeTitle で始まるか。
func (a *NativeAdapter) NativeTitle(title string) bool { return strings.HasPrefix(title, NativeTitle) }

// NativeScreen は SetNativeScreen した画面。していなければ ok=false。
func (a *NativeAdapter) NativeScreen(pid, _ int) (agent.NativeView, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	text, ok := a.screens[pid]
	if !ok {
		return agent.NativeView{}, false
	}
	lines := strings.Split(text, "\n")
	v := agent.NativeView{Screen: agent.NewScreen(lines)}
	if a.FakeAdapter.Detect(v.Screen).State == agent.StateAsking {
		v.AskQuestions = max(a.questions, 1)
	}
	return v, true
}

// NativeDelivered は since 以降に AddDelivered された text か。
func (a *NativeAdapter) NativeDelivered(pid int, text string, since time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range a.delivered[pid] {
		if d.text == text && !d.at.Before(since) {
			return true
		}
	}
	return false
}
