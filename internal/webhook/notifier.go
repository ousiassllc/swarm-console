package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Options は Notifier の依存。Config・ReportDir・StatePath は必須で、残りは空なら既定。
type Options struct {
	Config    config.Webhook
	ReportDir string
	StatePath string // 台帳（送信済みの回）の保存先
	Version   string // User-Agent に入れる
	Logger    *slog.Logger
	FS        pane.ReportFS // 最終報告ファイルの様子（Lstat だけ使う）。nil なら OSReportFS
	Client    *http.Client  // nil なら Config.Timeout のクライアント（リダイレクトは追わない）
	// Sleep は再送の間隔の待ち。ctx が終わったらそのエラーで戻る。テストは待たずに間隔を記録する実装に替える。
	Sleep func(ctx context.Context, d time.Duration) error
}

// Notifier はスナップショットを見て、ペインが working → idle になり最終報告が書かれた回ごとに 1 回だけ webhook を送る。
// pane.SnapshotObserver として registry に渡し、Start で送信を始める。
type Notifier struct {
	o Options

	mu      sync.Mutex
	entries map[string]*entry // キーは tmux のペイン ID
	ctx     context.Context   // Start が決める。nil の間は送らず、待ちのままにする
	closed  bool              // wait が呼ばれた。以後は新しく送らない
	wg      sync.WaitGroup
}

var _ pane.SnapshotObserver = (*Notifier)(nil)

// New は台帳を読んで Notifier を作る。台帳が壊れていても、空から始める（警告のログだけ）。
// 呼び出し側は Config.Enabled() のときだけ作る（URL が無ければ何も作らない）。
func New(o Options) (*Notifier, error) {
	switch {
	case !o.Config.Enabled():
		return nil, errors.New("webhook: url が設定されていません")
	case o.ReportDir == "" || o.StatePath == "":
		return nil, errors.New("webhook: ReportDir と StatePath が必要です")
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.FS == nil {
		o.FS = pane.OSReportFS{}
	}
	if o.Client == nil {
		o.Client = newClient(o.Config.Timeout)
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	entries, err := loadLedger(o.StatePath)
	if err != nil {
		o.Logger.Warn("webhook の台帳を読めませんでした。空から始めます", "path", o.StatePath, "err", err)
	}
	return &Notifier{o: o, entries: entries}, nil
}

// Start は送信を始める（これ以前に作った回の待ちは、ここから送れる）。ctx が終わると再送の待ち・通信を中断し、
// 返した関数が進行中の送信の終了を待つ。中断した回は台帳に残るので、再起動後に続きから送る。
func (n *Notifier) Start(ctx context.Context) (wait func()) {
	n.mu.Lock()
	n.ctx = ctx
	n.mu.Unlock()
	return func() {
		n.mu.Lock()
		n.closed = true
		n.mu.Unlock()
		n.wg.Wait()
	}
}

// Observe はスナップショット 1 つ分を処理する。pane.SnapshotObserver。通信は別 goroutine で行い、すぐ戻る。
// ペインが 1 つも無いスナップショット（tmux に接続できない・一覧が空）は、状態の判定に使わない。
func (n *Notifier) Observe(snap *pane.Snapshot) {
	if snap == nil || len(snap.Panes) == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	dirty := false
	present := make(map[string]bool, len(snap.Panes))
	for i := range snap.Panes {
		p := &snap.Panes[i]
		present[p.ID] = true
		if n.step(snap, p) {
			dirty = true
		}
	}
	for id := range n.entries {
		if !present[id] {
			delete(n.entries, id) // 消えたペイン（tmux の ID は再起動で使い回されるので、残さない）
			dirty = true
		}
	}
	if dirty {
		n.saveLocked()
	}
}

// step は 1 ペインの回を進める。台帳が変わったら true。
//
//	working・stalled → 回を始める（そのときの最終報告を Base に覚える）
//	idle（回あり）   → 待ちに移る。最終報告が Base から変わっていれば送る
//	shell            → 回を捨てる
//	asking・unknown  → 変えない（質問や一時的な判定不能をはさんでも、同じ回）
func (n *Notifier) step(snap *pane.Snapshot, p *pane.PaneState) bool {
	e := n.entries[p.ID]
	switch p.State {
	case agent.StateWorking, agent.StateStalled:
		if e != nil && e.Phase == phaseWorking {
			return false
		}
		n.entries[p.ID] = &entry{Phase: phaseWorking, Base: n.stat(p)}
		return true
	case agent.StateShell:
		if e == nil {
			return false
		}
		delete(n.entries, p.ID)
		return true
	case agent.StateIdle:
		if e == nil {
			return false
		}
		dirty := false
		if e.Phase == phaseWorking {
			e.Phase, e.ChangedAt, e.EventID = phaseWaiting, snap.At, eventID(p.Session, p.ID, snap.At)
			dirty = true
		}
		if e.Phase == phaseWaiting && !e.sending {
			if cur := n.stat(p); cur.Exists && cur != e.Base {
				n.deliverLocked(p, e)
			}
		}
		return dirty
	}
	return false
}

// stat は最終報告ファイルの今の様子。無い・通常のファイルでない（リンク）なら「無し」。
func (n *Notifier) stat(p *pane.PaneState) reportStat {
	fi, err := n.o.FS.Lstat(pane.ReportPath(n.o.ReportDir, p.ReportPos()))
	if err != nil || !fi.Mode().IsRegular() {
		return reportStat{}
	}
	return reportStat{Exists: true, ModNS: fi.ModTime().UnixNano(), Size: fi.Size()}
}

// deliverLocked は e の回を別 goroutine で送る（n.mu を持って呼ぶ）。終わったら台帳を sent・failed にする。
func (n *Notifier) deliverLocked(p *pane.PaneState, e *entry) {
	ctx := n.ctx
	if ctx == nil || n.closed || ctx.Err() != nil {
		return
	}
	e.sending = true
	pl := newPayload(n.o.ReportDir, p, e)
	paneID := p.ID
	n.wg.Go(func() { n.finish(paneID, pl.EventID, n.send(ctx, pl)) })
}

// finish は送信の結果を台帳に反映する。中断（終了処理）のときは待ちのままにして、再起動後に送り直す。
// その間に回が変わっていたら（ペインが working に戻った・消えた）、何も書かない。
func (n *Notifier) finish(paneID, id string, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	e := n.entries[paneID]
	if e == nil || e.EventID != id {
		return
	}
	e.sending = false
	switch {
	case err == nil:
		e.Phase = phaseSent
	case errors.Is(err, errGaveUp):
		e.Phase = phaseFailed
	default:
		return
	}
	n.saveLocked()
}

func (n *Notifier) saveLocked() {
	if err := saveLedger(n.o.StatePath, n.entries); err != nil {
		n.o.Logger.Warn("webhook の台帳を書けませんでした", "path", n.o.StatePath, "err", err)
	}
}
