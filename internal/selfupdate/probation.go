package selfupdate

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

const (
	// maxBoots は、増やした boots がこの値以上になる起動（3 回目）で戻す（P-55）。
	maxBoots = 3
	// ConfirmAfter は、待ち受けを始めてから試運転を確定するまでの時間（FR-UPD-17）。
	ConfirmAfter = 60 * time.Second
)

// Decision は起動の最初の試運転の判定（FR-UPD-17）。
type Decision struct {
	// Rollback が true なら、旧バイナリへ戻したので終了コード 1 で終了する（systemd が旧バイナリを起動する）。
	Rollback bool
	// Trial は試運転中のとき（マーカーが残っている）だけ non-nil。
	Trial *Trial
}

// Trial は試運転中の入れ替え。待ち受けを始めてから 60 秒生きたら確定してマーカーを消す。
type Trial struct {
	From, To string

	dir    string
	logger *slog.Logger
}

// Probation は serve の起動の最初（設定の読み込みより前）に呼ぶ。dir は置き場（Dir(home)）、version は自分の版。
// マーカーが無ければ通常どおり。to が自分の版でなければ（入れ替えが起きなかった）マーカーを消して通常どおり。
// 自分の版なら boots を 1 増やして書き戻し、3 以上なら .old を戻して .update-failed を書く。
func Probation(dir, version string, clk clock.Clock, logger *slog.Logger) Decision {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	p, exists, ok := readPending(dir)
	if !exists {
		return Decision{}
	}
	markerPath := filepath.Join(dir, pendingName)
	if !ok || p.To != version {
		_ = os.Remove(markerPath) // 形が不正、または入れ替えが起きなかった
		return Decision{}
	}
	p.Boots++
	if p.Boots < maxBoots {
		if err := writePending(dir, p); err != nil {
			// 数えられないので、これ以上は試運転にしない（戻しの判定ができないまま起動を妨げない）。
			_ = os.Remove(markerPath)
			logger.Error("試運転のマーカーを書き戻せませんでした。試運転を打ち切ります", "from", p.From, "to", p.To)
			return Decision{}
		}
		return Decision{Trial: &Trial{From: p.From, To: p.To, dir: dir, logger: logger}}
	}
	return rollback(dir, p, clk, logger)
}

// rollback は 3 回目の起動で .old を戻す。.old が無い・戻せないときはマーカーを消して通常起動を続ける（以後は数えない）。
func rollback(dir string, p pending, clk clock.Clock, logger *slog.Logger) Decision {
	markerPath := filepath.Join(dir, pendingName)
	if err := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, binName)); err != nil {
		_ = os.Remove(markerPath)
		logger.Error("旧バイナリへ戻せませんでした。このまま起動を続けます", "from", p.From, "to", p.To)
		return Decision{}
	}
	_ = os.Remove(markerPath)
	if err := writeFailed(dir, p.To, clk.Now()); err != nil {
		logger.Error("戻した版の記録を書けませんでした", "version", p.To)
	}
	logger.Error("新しい版が起動に失敗し続けたので、前の版へ戻して終了します", "from", p.From, "to", p.To)
	return Decision{Rollback: true}
}

// Run は ready（待ち受けの開始）を待ち、そこから 60 秒（clk 経由）プロセスが終了しなければマーカーを消す。
// ctx が終わる（終了する）と、確定せずに戻る（マーカーは残る）。確定したら true を返す。
func (t *Trial) Run(ctx context.Context, clk clock.Clock, ready <-chan struct{}) bool {
	select {
	case <-ready:
	case <-ctx.Done():
		return false
	}
	select {
	case <-clk.After(ConfirmAfter):
	case <-ctx.Done():
		return false
	}
	if err := removeIfExists(filepath.Join(t.dir, pendingName)); err != nil {
		t.logger.Error("試運転のマーカーを消せませんでした", "from", t.From, "to", t.To)
		return false
	}
	t.logger.Info("新しい版の試運転が済みました", "from", t.From, "to", t.To)
	return true
}
