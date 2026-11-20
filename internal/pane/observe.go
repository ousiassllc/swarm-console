package pane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

const (
	// CaptureLines はスナップショット・操作の確認で取る画面の行数（Screen.Lines の最大）。
	CaptureLines = 80
	activityMax  = 120 // Activity の字数上限
)

// shellCommands は前面コマンドがシェルかの判定に使う名前（先頭の `-`（ログインシェル）は除く）。
var shellCommands = map[string]bool{
	"bash": true, "zsh": true, "sh": true, "fish": true, "dash": true, "ksh": true,
	"ash": true, "csh": true, "tcsh": true, "nu": true, "elvish": true, "xonsh": true, "pwsh": true,
}

func isShellCommand(cmd string) bool {
	return shellCommands[filepath.Base(strings.TrimPrefix(strings.TrimSpace(cmd), "-"))]
}

// observation は 1 ペインの 1 回分の観測（時間に依存する判定の前）。
type observation struct {
	agentName string
	state     agent.State // working・asking・idle・shell・unknown（stalled は後で決める）
	activity  string
	elapsed   string
	hash      string
	readable  bool
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// observe は 1 ペインの画面を読み、エージェントと状態を決める（コンポーネント設計 §4）。
func (r *SnapshotRegistry) observe(ctx context.Context, p tmux.PaneInfo) observation {
	if p.Dead {
		return observation{state: agent.StateUnknown, readable: true}
	}
	text, err := r.deps.Tmux.Capture(ctx, p.ID, CaptureLines)
	if err != nil {
		r.log.Debug("capture に失敗", "pane", p.ID, "err", err)
		return observation{state: agent.StateUnknown}
	}
	screen := ScreenOf(text)
	name, ad, pid := r.identify(p, screen)
	if ad == nil {
		o := observation{state: agent.StateUnknown, hash: hashText(strings.Join(screen.Lines, "\n")), readable: true}
		if isShellCommand(p.CurrentCmd) {
			o.state = agent.StateShell
			r.deps.Unaccepted.Release(p.ID) // エージェントが終わった。件を送ったあとも見ていたなら、やめる（Issue #115）
		}
		return o
	}
	det := ad.Detect(screen)
	o := observation{
		agentName: name,
		state:     agent.StateIdle,
		activity:  det.Activity,
		elapsed:   det.Elapsed,
		hash:      hashText(ad.StableText(screen.Lines)),
		readable:  true,
	}
	switch det.State {
	case agent.StateAsking:
		o.state = agent.StateAsking
		if c, ok := ad.ParseCard(screen); ok && c.Question != "" {
			o.activity = c.Question
		}
	case agent.StateWorking:
		o.state = agent.StateWorking
	}
	o.state = r.confirmByLog(p.ID, ad, pid, o.state)
	return o
}

const (
	// logWorkWindow は、エージェントのログの最後の作業の行からこの間は、画面が idle でも作業中とみなす（Issue #113）。
	// 作業中のログは数分あくことがある（長いツールの実行中）ので、あいたら idle に戻り、次の作業の行でまた working になる。
	logWorkWindow = 2 * time.Minute
	// unacceptedTTL は、受理を確かめられなかった指示の記録を、最後の作業の行（無ければ Enter）からこの間たっても作業の跡が無ければ捨てる。
	// 止まっていたペインを、いつまでもログの確認の対象にしない。件を送ったあと（Retain）のペインには適用しない（Issue #115）。
	unacceptedTTL = 10 * time.Minute
	// retainedPollInterval は、件を送ったあとも見続けるペイン（Retain）のログを読む間隔。それまでは前に読んだ結果を使う。
	// 見続ける間は何も起きないことが多く、長く待つ作業（CI 待ちなど）が続いても、2 件目が少し遅れて飛べばよいので、5 秒ごとの更新より粗くする（P-107）。
	retainedPollInterval = 30 * time.Second
)

// confirmByLog は、指示の Enter を送ったのに受理を画面で確かめられなかったペイン（Unaccepted）だけ、エージェントのログと会話の記録で作業中かを確かめる。
// omp は負荷が高いと画面の描き直しが止まり、作業中でも起動画面のまま idle と読めるため（Issue #113）。
// 画面が working・asking と読めたら画面が回復したので記録を捨てる。それ以外の状態・記録の無いペインは何も変えない。
// 作業中と読むのは、(1) ログの最後の作業の行から logWorkWindow 以内、(2) 会話の記録の最後が番の途中（TurnProbe。長いコマンドの実行中はモデルの呼び出しの行が出ないので、(2) が作業中を保つ。Issue #118）。
// (2) は時間で打ち切らない（エージェントのプロセスが生きている間）。どちらの跡も無い（止まっている）なら idle のままにする。
// 件を送ったあと（Retain）のペインは、10 分の上限で捨てず、retainedPollInterval おきに読む。別のプロセス（pid）になっていたら捨てる（Issue #115）。
func (r *SnapshotRegistry) confirmByLog(id string, ad agent.Adapter, pid int, state agent.State) agent.State {
	e, marked := r.deps.Unaccepted.probe(id)
	if !marked {
		return state
	}
	if state == agent.StateWorking || state == agent.StateAsking {
		r.deps.Unaccepted.Clear(id)
		return state
	}
	wp, hasLog := ad.(agent.WorkProbe)
	tp, hasTurn := ad.(agent.TurnProbe)
	if state != agent.StateIdle || (!hasLog && !hasTurn) || pid == 0 {
		return state
	}
	if e.PID != 0 && e.PID != pid { // 同じペイン ID で別のエージェント（tmux の再起動で ID が使い回された・起動し直した）
		r.deps.Unaccepted.Clear(id)
		return state
	}
	now := r.clk.Now()
	res := e.probeResult
	if !e.Retained || e.Checked.IsZero() || now.Sub(e.Checked) >= retainedPollInterval {
		res = probeResult{PID: pid, Checked: now}
		if hasLog {
			res.Last, res.Worked = wp.LastWork(pid, e.Enter)
		}
		if hasTurn {
			res.TurnLast, res.InTurn, res.TurnOK = tp.Turn(pid, e.Enter)
		}
		r.deps.Unaccepted.setProbe(id, res)
	}
	if res.TurnOK && res.InTurn {
		return agent.StateWorking
	}
	last := e.Enter
	if res.Worked && res.Last.After(last) {
		last = res.Last
	}
	if res.TurnOK && res.TurnLast.After(last) {
		last = res.TurnLast
	}
	if !e.Retained && now.Sub(last) > unacceptedTTL {
		r.deps.Unaccepted.Clear(id)
		return state
	}
	if res.Worked && now.Sub(res.Last) <= logWorkWindow {
		return agent.StateWorking
	}
	return state
}

// identify はエージェントを特定する。まず /proc のプロセス木（process_match）、取れなければ画面の特徴（P-06）。
// 前面コマンドがシェルなら、画面に残った過去の表示で誤判定しないよう画面は見ない。木を読めるときは、木にエージェントが
// いない（木が空でない）ことも条件にし、読めないときは前面コマンドだけで決める（/proc が読めない環境でも pane_close が誤って
// exit_timeout にならないように。Issue #35）。
// 3 つ目の返り値は、プロセス木から見つけたエージェントの pid（画面で決めたときは 0）。
func (r *SnapshotRegistry) identify(p tmux.PaneInfo, screen agent.Screen) (string, agent.Adapter, int) {
	procs, err := r.deps.Host.ProcTree(p.PID)
	if err != nil {
		if isShellCommand(p.CurrentCmd) {
			return "", nil, 0
		}
	} else {
		for _, name := range r.names {
			ad := r.deps.Adapters[name]
			for _, pr := range procs {
				if len(pr.Argv) > 0 && ad.MatchProcess(pr.Argv) {
					return name, ad, pr.PID
				}
			}
		}
		if len(procs) > 0 && isShellCommand(p.CurrentCmd) {
			return "", nil, 0
		}
	}
	for _, name := range r.names {
		if ad := r.deps.Adapters[name]; ad.MatchScreen(screen) {
			return name, ad, 0
		}
	}
	return "", nil, 0
}
