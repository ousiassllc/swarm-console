// Package panetest は pane・pane/ops のテストで使う、インメモリのフェイク（Tmux・Host・Adapter・NoteStore・ReportFS）を置く。
package panetest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// Op は FakeTmux が受けた書き込み系の操作 1 つ。
type Op struct {
	Kind string // keys | literal | paste | title
	ID   string
	Text string   // literal・paste の本文、title のタイトル
	Keys []string // keys のキー名
}

// String はテストの失敗表示・比較用の短い表現。
func (o Op) String() string {
	switch o.Kind {
	case "keys":
		return "keys " + o.ID + " " + strings.Join(o.Keys, " ")
	default:
		return fmt.Sprintf("%s %s %q", o.Kind, o.ID, o.Text)
	}
}

// FakeTmux は pane.Tmux のフェイク。画面はテストが Screens に入れ、書き込み操作は Ops に記録される。
type FakeTmux struct {
	mu          sync.Mutex
	panes       []tmux.PaneInfo
	screens     map[string]string
	captureErr  map[string]error
	listErr     error
	serverStart string
	version     string
	ops         []Op
	listCalls   int
	onOp        func(Op)
	onCapture   func()
	onList      func()
}

// NewTmux は空のフェイクを作る。
func NewTmux() *FakeTmux {
	return &FakeTmux{screens: map[string]string{}, captureErr: map[string]error{}, serverStart: "1000", version: "tmux 3.4"}
}

// SetPanes は list-panes の結果を差し替える。
func (f *FakeTmux) SetPanes(p ...tmux.PaneInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes = append([]tmux.PaneInfo(nil), p...)
}

// SetScreen は id の画面を差し替える。
func (f *FakeTmux) SetScreen(id, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screens[id] = text
}

// SetCaptureErr は id の capture-pane を失敗させる（nil で戻す）。
func (f *FakeTmux) SetCaptureErr(id string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captureErr[id] = err
}

// SetListErr は list-panes を失敗させる（nil で戻す）。
func (f *FakeTmux) SetListErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listErr = err
}

// SetServerStart はサーバー起動時刻を差し替える。
func (f *FakeTmux) SetServerStart(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serverStart = s
}

// OnOp は書き込み操作が記録された直後に呼ばれるフック（擬似エージェントの反応を書く）。フック内から SetScreen を呼んでよい。
func (f *FakeTmux) OnOp(fn func(Op)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onOp = fn
}

// OnCapture は capture-pane の呼び出しごとに、画面を返す前に呼ばれるフック（描き直しの遅れを時計に合わせて再現する）。
// フック内から SetScreen を呼んでよい。
func (f *FakeTmux) OnCapture(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onCapture = fn
}

// OnList は list-panes の呼び出しごとに呼ばれるフック（singleflight の確認用に待たせられる）。
func (f *FakeTmux) OnList(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onList = fn
}

// Ops は記録された書き込み操作のコピー。
func (f *FakeTmux) Ops() []Op {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Op(nil), f.ops...)
}

// OpStrings は Ops を String にしたもの。
func (f *FakeTmux) OpStrings() []string {
	ops := f.Ops()
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.String()
	}
	return out
}

// ListCalls は list-panes が呼ばれた回数。
func (f *FakeTmux) ListCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}

// CountKeys は記録された keys 操作のうち、キー名 key を含む回数。
func (f *FakeTmux) CountKeys(key string) int {
	n := 0
	for _, o := range f.Ops() {
		if o.Kind != "keys" {
			continue
		}
		for _, k := range o.Keys {
			if k == key {
				n++
			}
		}
	}
	return n
}

func (f *FakeTmux) record(o Op) {
	f.mu.Lock()
	f.ops = append(f.ops, o)
	hook := f.onOp
	f.mu.Unlock()
	if hook != nil {
		hook(o)
	}
}

// ListPanes は設定された一覧のうち sessions に含まれるものを返す。
func (f *FakeTmux) ListPanes(_ context.Context, sessions []string) ([]tmux.PaneInfo, error) {
	f.mu.Lock()
	f.listCalls++
	hook, err := f.onList, f.listErr
	var out []tmux.PaneInfo
	for _, p := range f.panes {
		for _, s := range sessions {
			if p.Session == s {
				out = append(out, p)
				break
			}
		}
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return out, err
}

// Capture は設定された画面を返す。lines は無視する（アダプタ側が最後の 80 行に切る）。
func (f *FakeTmux) Capture(_ context.Context, id string, _ int) (string, error) {
	f.mu.Lock()
	hook := f.onCapture
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.captureErr[id]; err != nil {
		return "", err
	}
	return f.screens[id], nil
}

// SendKeys は記録する。
func (f *FakeTmux) SendKeys(_ context.Context, id string, keys ...string) error {
	f.record(Op{Kind: "keys", ID: id, Keys: append([]string(nil), keys...)})
	return nil
}

// SendLiteral は記録する。
func (f *FakeTmux) SendLiteral(_ context.Context, id, text string) error {
	f.record(Op{Kind: "literal", ID: id, Text: text})
	return nil
}

// Paste は記録する。
func (f *FakeTmux) Paste(_ context.Context, id, text string) error {
	f.record(Op{Kind: "paste", ID: id, Text: text})
	return nil
}

// SetTitle は記録する。
func (f *FakeTmux) SetTitle(_ context.Context, id, title string) error {
	f.record(Op{Kind: "title", ID: id, Text: title})
	return nil
}

// ServerStart は設定された起動時刻。
func (f *FakeTmux) ServerStart(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.serverStart, nil
}

// Version は tmux -V の出力。
func (f *FakeTmux) Version(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, nil
}
