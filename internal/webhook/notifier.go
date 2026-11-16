package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// Options は Notifier の依存。Config・ReportDir・StatePath は必須で、残りは空なら既定。
type Options struct {
	Config    config.Webhook // 送り先は Config.Target(セッション名) で引く（[webhook] とセッションごとの表）
	ReportDir string
	StatePath string // 台帳（回と、送る約束をした項目）の保存先
	Version   string // User-Agent に入れる
	Logger    *slog.Logger
	FS        pane.ReportFS // 最終報告ファイルの様子（Lstat だけ使う）。nil なら OSReportFS
	Client    *http.Client  // nil なら送り先の Timeout のクライアント（リダイレクトは追わない）
	// Sleep は再送の間隔の待ち。ctx が終わったらそのエラーで戻る。テストは待たずに間隔を記録する実装に替える。
	Sleep func(ctx context.Context, d time.Duration) error
}

// Notifier はスナップショットを見て、ペインが working → idle になり最終報告が書かれた回ごとに 1 件の送信（outbox の項目）を作り、
// 送り終える（または諦める）まで届ける。pane.SnapshotObserver として registry に渡し、Start で送信を始める。
//
// 回（ペインごとの entries）と送信（outbox）は独立している。回は「いつ項目を作るか」だけを決め、項目を作ったら回は done になる。
// 項目はペインの状態に左右されず、本文も回数の上限も再起動をまたいで引き継ぐ。
type Notifier struct {
	o Options

	mu          sync.Mutex
	entries     map[string]*entry // キーは tmux のペイン ID
	outbox      map[string]*item  // キーは event_id
	serverStart string            // 台帳に書いた tmux サーバーの起動時刻
	absent      map[string]int    // ペイン ID → 続けてスナップショットに無かった回数（保存しない）
	ctx         context.Context   // Start が決める。nil の間は送らず、待ちのままにする
	closed      bool              // wait が呼ばれた。以後は新しく送らない
	wg          sync.WaitGroup
}

var (
	_ pane.SnapshotObserver = (*Notifier)(nil)
	_ slog.LogValuer        = (*Notifier)(nil)
)

// New は台帳を読んで Notifier を作る。台帳が壊れていても、空から始める（警告のログだけ）。
// 呼び出し側は Config.Enabled() のときだけ作る（送り先が 1 つも無ければ何も作らない）。
func New(o Options) (*Notifier, error) {
	switch {
	case !o.Config.Enabled():
		return nil, errors.New("webhook: 送り先の url が設定されていません")
	case o.ReportDir == "" || o.StatePath == "":
		return nil, errors.New("webhook: ReportDir と StatePath が必要です")
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.FS == nil {
		o.FS = pane.OSReportFS{}
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	led, err := loadLedger(o.StatePath)
	if err != nil {
		o.Logger.Warn("webhook の台帳を読めませんでした。空から始めます", "path", o.StatePath, "err", err)
	}
	return &Notifier{o: o, entries: led.Panes, outbox: led.Outbox, serverStart: led.ServerStart, absent: map[string]int{}}, nil
}

// Start は送信を始める。台帳から読んだ項目と、これ以前に作った項目の送信がここから動く。ctx が終わると再送の待ち・通信を中断し、
// 返した関数が進行中の送信の終了を待つ。中断した項目は台帳に残るので、再起動後に同じ本文で続きから送る。
func (n *Notifier) Start(ctx context.Context) (wait func()) {
	n.mu.Lock()
	n.ctx = ctx
	for _, it := range n.outbox {
		n.startItemLocked(it)
	}
	n.mu.Unlock()
	return func() {
		n.mu.Lock()
		n.closed = true
		n.mu.Unlock()
		n.wg.Wait()
	}
}

// String・GoString・LogValue は、設定（URL・鍵・トークン）を持つ Notifier を fmt・slog に渡しても何も出さないための固定の文。
// Options は unexported のフィールドに入っているので、fmt は Secret の String を呼ばず、値をそのまま出してしまう。
func (*Notifier) String() string { return "webhook.Notifier" }

// GoString は fmt の %#v に使われる。
func (*Notifier) GoString() string { return "webhook.Notifier" }

// LogValue は slog の属性に使われる。
func (*Notifier) LogValue() slog.Value { return slog.StringValue("webhook.Notifier") }

func (n *Notifier) saveLocked() {
	f := ledgerFile{ServerStart: n.serverStart, Panes: n.entries, Outbox: n.outbox}
	if err := saveLedger(n.o.StatePath, f); err != nil {
		n.o.Logger.Warn("webhook の台帳を書けませんでした", "path", n.o.StatePath, "err", err)
	}
}
