package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/swarm-console/test/fakeagent/fakeagenttest"
)

// Pane は panes_status の 1 ペイン。
type Pane struct {
	Pos        string         `json:"pos"`
	ID         string         `json:"id"`
	Agent      *string        `json:"agent"`
	State      string         `json:"state"`
	Title      string         `json:"title"`
	Cwd        string         `json:"cwd"`
	Elapsed    *string        `json:"elapsed"`
	Activity   *string        `json:"activity"`
	ChangedAgo *string        `json:"changed_ago"`
	Note       map[string]any `json:"note"`
}

// Warning は panes_status の warnings の 1 件。
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Status は panes_status の返り値。
type Status struct {
	At     string `json:"at"`
	Layout struct {
		Rows int   `json:"rows"`
		Cols []int `json:"cols"`
	} `json:"layout"`
	Panes    []Pane    `json:"panes"`
	Warnings []Warning `json:"warnings"`
}

// Status は ops クライアントで panes_status を呼ぶ。
func (h *H) Status() Status {
	h.T.Helper()
	var s Status
	h.Ops().Must("panes_status", nil).Decode(h.T, &s)
	return s
}

// StatusOf は ops クライアントで session を指定して panes_status を呼ぶ（許可が複数のトークンは session が必須）。
func (h *H) StatusOf(session string) Status {
	h.T.Helper()
	var s Status
	h.Ops().Must("panes_status", map[string]any{"session": session}).Decode(h.T, &s)
	return s
}

// Pane は ID のペインの現在の様子。無ければ false。
func (h *H) Pane(id string) (Pane, bool) {
	h.T.Helper()
	for _, p := range h.Status().Panes {
		if p.ID == id {
			return p, true
		}
	}
	return Pane{}, false
}

// PosOf は ID のペインの現在の位置。
func (h *H) PosOf(id string) string {
	h.T.Helper()
	p, ok := h.Pane(id)
	if !ok {
		h.T.Fatalf("ペイン %s が panes_status にありません", id)
	}
	return p.Pos
}

// WaitState は ID のペインが state になるまで待ち、そのときの様子を返す。
func (h *H) WaitState(id, state string) Pane {
	h.T.Helper()
	var got Pane
	h.Eventually(fmt.Sprintf("%s が %s になる", id, state), func() bool {
		p, ok := h.Pane(id)
		got = p
		return ok && p.State == state
	})
	return got
}

// NewWindow は main セッションに新しいウィンドウを足してペイン ID を返す（command 空ならシェル）。
func (h *H) NewWindow(command string) string {
	h.T.Helper()
	args := []string{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", Session + ":"}
	if command != "" {
		args = append(args, command)
	}
	return strings.TrimSpace(h.TM.Run(args...))
}

// NewShell はシェルのペインを足し、shell と判定されるまで待つ。
func (h *H) NewShell() string {
	h.T.Helper()
	id := h.NewWindow("")
	h.WaitState(id, "shell")
	return id
}

// LogPath は ID のペインで動く擬似エージェントの --log のパス。
func (h *H) LogPath(id string) string { return filepath.Join(h.Logs, id+".log") }

// FakeCommand は擬似エージェントを起動するシェルのコマンド行（--log は $TMUX_PANE で決まる）。
func (h *H) FakeCommand(agentName, scenario string) string {
	return fmt.Sprintf(`%s %s --log "%s/$TMUX_PANE.log"`, agentName, scenario, h.Logs)
}

// NewAgent は擬似エージェントを直接動かすウィンドウを足し、READY が出るまで待つ。ペイン ID を返す。
func (h *H) NewAgent(agentName, scenario string) string {
	h.T.Helper()
	id := h.NewWindow("exec " + h.FakeCommand(agentName, scenario))
	h.TM.WaitForText(id, "READY")
	return id
}

// TypeFake はシェルのペインへ擬似エージェントの起動を打ち、READY を待つ。
func (h *H) TypeFake(id, agentName, scenario string) {
	h.T.Helper()
	h.Type(id, h.FakeCommand(agentName, scenario))
	h.TM.WaitForText(id, "READY")
}

// Type はペインへ 1 行を打つ。
func (h *H) Type(id, line string) {
	h.T.Helper()
	h.TM.Run("send-keys", "-t", id, "-l", line)
	h.TM.Run("send-keys", "-t", id, "Enter")
}

// Events は ID のペインの擬似エージェントの --log。
func (h *H) Events(id string) []fakeagenttest.Event {
	h.T.Helper()
	ev, err := fakeagenttest.ReadLog(h.LogPath(id))
	if err != nil {
		h.T.Fatalf("--log を読めません: %v", err)
	}
	return ev
}

// WaitEvents は --log が pred を満たすまで待つ。
func (h *H) WaitEvents(id, what string, pred func([]fakeagenttest.Event) bool) []fakeagenttest.Event {
	h.T.Helper()
	var ev []fakeagenttest.Event
	h.Eventually(what, func() bool {
		var err error
		if ev, err = fakeagenttest.ReadLog(h.LogPath(id)); err != nil {
			return false // 書き込み途中。次の確認で読み直す
		}
		return pred(ev)
	})
	return ev
}

// Kinds は --log の出来事の種別だけの列。
func Kinds(ev []fakeagenttest.Event) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = e.Kind
	}
	return out
}

// Refresh は ops の失敗する操作（シェルへの send_escape）でスナップショットを取り直させる。副作用はない。
func (h *H) Refresh(shellPane string) {
	h.T.Helper()
	r := h.Ops().Call("send_escape", map[string]any{"pane": shellPane, "confirm": true})
	if code := r.Code(h.T); code != "invalid_state" {
		h.T.Fatalf("Refresh 用の操作が invalid_state 以外: %s", r.Text)
	}
}

// NewFrozenAsk は omp の質問カードを出したまま入力に一切反応しない擬似エージェント（omp ask-frozen）を動かすペインを足す。
// カーソルが動かないので cursor_mismatch を起こせる。受け取った入力は --raw にそのまま追記されるので、
// Enter（CR・LF）が送られたかを確かめられる。ペイン ID とキー入力の記録ファイルを返す。
// 入力を記録するプログラムは cat の別名ではなく擬似エージェントにする。cat が単一バイナリ（multicall。
// uutils coreutils など）のときは argv[0] が omp だと動かず、ペインがすぐ消えるため。
func (h *H) NewFrozenAsk() (id, keysFile string) {
	h.T.Helper()
	keysFile = filepath.Join(h.Logs, "frozen-keys.bin")
	id = h.NewWindow("exec " + h.FakeCommand("omp", "ask-frozen") + fmt.Sprintf(" --raw %q", keysFile))
	h.TM.WaitForText(id, "READY")
	return id, keysFile
}
