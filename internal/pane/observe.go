package pane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

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
	name, ad := r.identify(p, screen)
	if ad == nil {
		o := observation{state: agent.StateUnknown, hash: hashText(strings.Join(screen.Lines, "\n")), readable: true}
		if isShellCommand(p.CurrentCmd) {
			o.state = agent.StateShell
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
	return o
}

// identify はエージェントを特定する。まず /proc のプロセス木（process_match）、取れなければ画面の特徴（P-06）。
// 前面コマンドがシェルなら、画面に残った過去の表示で誤判定しないよう画面は見ない。木を読めるときは、木にエージェントが
// いない（木が空でない）ことも条件にし、読めないときは前面コマンドだけで決める（/proc が読めない環境でも pane_close が誤って
// exit_timeout にならないように。Issue #35）。
func (r *SnapshotRegistry) identify(p tmux.PaneInfo, screen agent.Screen) (string, agent.Adapter) {
	procs, err := r.deps.Host.ProcTree(p.PID)
	if err != nil {
		if isShellCommand(p.CurrentCmd) {
			return "", nil
		}
	} else {
		for _, name := range r.names {
			ad := r.deps.Adapters[name]
			for _, pr := range procs {
				if len(pr.Argv) > 0 && ad.MatchProcess(pr.Argv) {
					return name, ad
				}
			}
		}
		if len(procs) > 0 && isShellCommand(p.CurrentCmd) {
			return "", nil
		}
	}
	for _, name := range r.names {
		if ad := r.deps.Adapters[name]; ad.MatchScreen(screen) {
			return name, ad
		}
	}
	return "", nil
}
