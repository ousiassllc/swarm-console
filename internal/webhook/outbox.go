package webhook

import (
	"context"
	"errors"
)

// startItemLocked は項目の送信を別 goroutine で始める（n.mu を持って呼ぶ）。
// Start の前・終了処理に入ったあと・送信中の項目では始めない（Start が、待っていた項目をまとめて始める）。
func (n *Notifier) startItemLocked(it *item) {
	ctx := n.ctx
	if ctx == nil || n.closed || ctx.Err() != nil || it.sending {
		return
	}
	it.sending = true
	n.wg.Go(func() { n.finish(it, n.send(ctx, it)) })
}

// finish は送信の結果を台帳に反映する。送れた・諦めたときは項目を消す（回は done なので、二度と作られない）。
// 中断（終了処理）のときは項目を残し、再起動後に同じ本文で続きから送る。
func (n *Notifier) finish(it *item, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	it.sending = false
	if err != nil && !errors.Is(err, errGaveUp) {
		return
	}
	delete(n.outbox, it.EventID)
	n.saveLocked()
}

// beginAttempt は次の送信（1 回）を始める前に、回数を増やして台帳に書く。再起動しても上限は引き継がれる。
// 上限まで使い切っていれば errGaveUp、ctx が終わっていればそのエラー（回数は増やさない）。
func (n *Notifier) beginAttempt(ctx context.Context, it *item) (attempt int, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return it.Attempts, err
	}
	if it.Attempts >= n.o.Config.MaxAttempts {
		return it.Attempts, errGaveUp
	}
	it.Attempts++
	n.saveLocked()
	return it.Attempts, nil
}

// refundAttempt は、終了で中断された送信の分を送った回数から戻して台帳に書く。
// 中断された通信は、受け手が受け取ったか分からないが、上限を使い切ったことにはしない。
func (n *Notifier) refundAttempt(it *item) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if it.Attempts > 0 {
		it.Attempts--
		n.saveLocked()
	}
}
