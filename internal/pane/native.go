package pane

import (
	"context"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Reading は 1 ペインの画面を、アダプタが読める形にしたもの（Issue #121）。
type Reading struct {
	Lines []string
	// Native は、エージェントが tern の画面（TSP）を出していて、Lines が capture-pane ではなく会話の記録から組み立てた画面であること。
	// この画面にはカーソル・入力欄・選んだ印が無い（質問カードのカーソルは最初の位置、入力欄は空として組み立てる）。
	Native bool
	// PID は Native のときのエージェントのプロセスの pid。
	PID int
	// AskQuestions は Native で質問カードが出ているときの、フォーム全体の質問の数。
	AskQuestions int
}

// ReadScreen は id のペインの画面を取り直し、ad が読める形で返す。tmux の失敗は tmux_error。
// ad が agent.Native で、ペインのタイトルが tern の画面データ（omp は `tsp;` で始まる）、かつ ad のプロセスが動いているときは、
// 会話の記録から組み立てた画面（Native）を返す。それ以外は capture-pane の画面そのまま。ad が nil（エージェントなし）のときも capture-pane。
// capture-pane の画面は見ない。tern の画面になる前に omp が描いた普通の画面の跡が、履歴に残るため（実機の history_size は 13〜15 行）。
func (r *SnapshotRegistry) ReadScreen(ctx context.Context, id string, lines int, ad agent.Adapter) (Reading, error) {
	if nat, ok := ad.(agent.Native); ok {
		if meta, err := r.deps.Tmux.PaneMeta(ctx, id); err == nil && nat.NativeTitle(meta.Title) {
			if pid := r.agentPID(meta.PID, ad); pid != 0 {
				if v, ok := nat.NativeScreen(pid, lines); ok {
					return Reading{Lines: v.Screen.Lines, Native: true, PID: pid, AskQuestions: v.AskQuestions}, nil
				}
			}
		}
	}
	text, err := r.deps.Tmux.Capture(ctx, id, lines)
	if err != nil {
		return Reading{}, TmuxError(err)
	}
	return Reading{Lines: SplitLines(text)}, nil
}

// agentPID はシェルの pid（shellPID）のプロセス木から、ad に当たるプロセスの pid を探す。見つからない・木を読めないときは 0。
func (r *SnapshotRegistry) agentPID(shellPID int, ad agent.Adapter) int {
	procs, err := r.deps.Host.ProcTree(shellPID)
	if err != nil {
		return 0
	}
	for _, pr := range procs {
		if len(pr.Argv) > 0 && ad.MatchProcess(pr.Argv) {
			return pr.PID
		}
	}
	return 0
}

// nativeScreen は、タイトルが tern の画面データで、エージェントのプロセス（pid）が分かっているとき、
// pid の会話の記録から組み立てた画面を返す。そうでなければ ok=false（capture-pane の画面をそのまま使う）。
// capture-pane の画面は見ない（ReadScreen と同じ理由）。
func nativeScreen(ad agent.Adapter, title string, pid int) (agent.Screen, bool) {
	nat, ok := ad.(agent.Native)
	if !ok || pid == 0 || !nat.NativeTitle(title) {
		return agent.Screen{}, false
	}
	v, ok := nat.NativeScreen(pid, CaptureLines)
	return v.Screen, ok
}

// paneTitle は PaneState.Title にするタイトル。tern の画面のペインのタイトルは画面データ（最後の 1 フレーム。数 KB の JSON）なので、
// panes_status・report・webhook に出さず空にする（Issue #121）。画面を組み立てられたかには関わらない
// （記録がまだ無い・読めない・omp が終わってシェルに戻った、でも出さない）。
func (r *SnapshotRegistry) paneTitle(title string) string {
	for _, ad := range r.deps.Adapters {
		if nat, ok := ad.(agent.Native); ok && nat.NativeTitle(title) {
			return ""
		}
	}
	return title
}
