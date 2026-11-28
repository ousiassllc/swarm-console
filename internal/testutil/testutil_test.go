package testutil_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/testutil"
)

func TestLockedBufferConcurrentWrites(t *testing.T) {
	var b testutil.LockedBuffer
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { _, _ = fmt.Fprint(&b, "x") })
	}
	wg.Wait()
	if got := b.String(); got != "xxxxxxxxxx" {
		t.Fatalf("String = %q", got)
	}
}

func TestEventuallyReturnsWhenConditionHolds(t *testing.T) {
	n := 0
	testutil.Eventually(t, "3 回目に成り立つ", func() bool { n++; return n >= 3 })
	if n != 3 {
		t.Fatalf("cond の呼び出し = %d 回, want 3", n)
	}
}

func TestRecvReturnsValue(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 7
	if got := testutil.Recv(t, ch, "値"); got != 7 {
		t.Fatalf("Recv = %d", got)
	}
}
