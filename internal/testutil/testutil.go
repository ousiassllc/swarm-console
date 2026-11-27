// Package testutil は複数パッケージのテストが共有する小さな補助（ロック付きバッファ・上限つきの待ち）を置く。
// テストからだけ import する。本体のパッケージからは import しない。
package testutil

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

const (
	// EventuallyTimeout は Eventually の上限。
	EventuallyTimeout = 20 * time.Second
	// RecvTimeout は Recv の上限。
	RecvTimeout = 10 * time.Second

	eventuallyInterval = 5 * time.Millisecond
)

// LockedBuffer は並行に書かれてよい bytes.Buffer（ログの出力先など）。ゼロ値で使える。
type LockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Write は io.Writer。
func (l *LockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// String は今までに書かれた内容を返す。
func (l *LockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Eventually は cond が true になるまで、上限（EventuallyTimeout）つきで繰り返し確かめる。
// 他の goroutine の進行を待つためのもので、固定時間の待ちには使わない。
// 上限に達したら what（何を待っていたか）を付けて t.Fatalf する。
func Eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	if cond() {
		return
	}
	deadline := time.NewTimer(EventuallyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(eventuallyInterval)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("条件を満たしませんでした（%s 待ち）: %s", EventuallyTimeout, what)
			return
		case <-tick.C:
			if cond() {
				return
			}
		}
	}
}

// Recv は ch から 1 つ受け取る。上限（RecvTimeout）までに来なければ what を付けて t.Fatalf する。
// ch が閉じられていればゼロ値を返す（通常の受信と同じ）。t.Cleanup の中から呼んでもよい（残りの Cleanup は走る）。
func Recv[T any](t testing.TB, ch <-chan T, what string) T {
	t.Helper()
	timer := time.NewTimer(RecvTimeout)
	defer timer.Stop()
	select {
	case v := <-ch:
		return v
	case <-timer.C:
		t.Fatalf("%s を待ちましたが、%s たっても来ません", what, RecvTimeout)
		panic("unreachable") // Fatalf は戻らない
	}
}
