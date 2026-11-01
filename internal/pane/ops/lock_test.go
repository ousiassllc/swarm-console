package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

type lockFixture struct {
	env *panetest.Env
	op  *Operator
}

func newFixture(t *testing.T) *lockFixture {
	t.Helper()
	env := panetest.NewEnv()
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: env.FS,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &lockFixture{env: env, op: New(reg)}
}

func TestLockSamePaneWaitsForRelease(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	unlock1, err := f.op.Lock(ctx, "%0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan func(), 1)
	go func() {
		u, err := f.op.Lock(ctx, "%0")
		if err != nil {
			t.Error(err)
		}
		got <- u
	}()
	panetest.WaitUntil(t, "2 つ目が lock_timeout の待ちに入る", func() bool { return f.env.Clock.Waiters() == 1 })
	select {
	case <-got:
		t.Fatal("先頭が解放される前に進んだ")
	default:
	}
	unlock1()
	unlock1() // 二重解放しても壊れない
	u2 := <-got
	u2()
	// 全員が解放したらエントリは残らない。
	f.op.locks.mu.Lock()
	defer f.op.locks.mu.Unlock()
	if n := len(f.op.locks.entries); n != 0 {
		t.Fatalf("ロックのエントリが %d 個残っている", n)
	}
}

func TestLockBusyTimeout(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	unlock, _ := f.op.Lock(ctx, "%0")
	defer unlock()
	errc := make(chan error, 1)
	go func() { _, err := f.op.Lock(ctx, "%0"); errc <- err }()
	panetest.WaitUntil(t, "待機に入る", func() bool { return f.env.Clock.Waiters() == 1 })
	f.env.Clock.Advance(59 * time.Second)
	select {
	case err := <-errc:
		t.Fatalf("lock_timeout 前に返った: %v", err)
	default:
	}
	f.env.Clock.Advance(time.Second)
	err := <-errc
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != apperr.BusyTimeout {
		t.Fatalf("err = %v", err)
	}
	// タイムアウトした待ち手は数に残らない: 先頭が解放すれば取れる。
	unlock()
	u, err := f.op.Lock(ctx, "%0")
	if err != nil {
		t.Fatal(err)
	}
	u()
}

func TestLockDifferentPanesRunConcurrently(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	u1, err := f.op.Lock(ctx, "%0")
	if err != nil {
		t.Fatal(err)
	}
	defer u1()
	u2, err := f.op.Lock(ctx, "%1")
	if err != nil {
		t.Fatalf("別のペインが待たされた: %v", err)
	}
	u2()
	if f.env.Clock.Waiters() != 0 {
		t.Fatal("待たずに取れるはず")
	}
}

func TestLockContextCancel(t *testing.T) {
	f := newFixture(t)
	unlock, _ := f.op.Lock(context.Background(), "%0")
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := f.op.Lock(ctx, "%0"); errc <- err }()
	panetest.WaitUntil(t, "待機に入る", func() bool { return f.env.Clock.Waiters() == 1 })
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestLockManyContendersNoDeadlock(t *testing.T) {
	f := newFixture(t)
	const n = 20
	done := make(chan struct{}, n)
	counter := 0
	for range n {
		go func() {
			u, err := f.op.Lock(context.Background(), "%0")
			if err != nil {
				t.Error(err)
				done <- struct{}{}
				return
			}
			counter++ // 排他が効いていれば -race が文句を言わない
			u()
			done <- struct{}{}
		}()
	}
	for range n {
		<-done
	}
	if counter != n {
		t.Fatalf("counter = %d", counter)
	}
}
