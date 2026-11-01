package reportvalues

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

// scriptRunner は呼ばれたコマンドを記録し、コマンドごとに用意した結果を返す偽の Runner。
type scriptRunner struct {
	mu    sync.Mutex
	calls []Command
	next  map[Command]result
}

type result struct {
	out    []byte
	reason string
}

func (r *scriptRunner) set(c Command, res result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next[c] = res
}

func (r *scriptRunner) Run(_ context.Context, c Command) ([]byte, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
	res := r.next[c]
	return res.out, res.reason
}

func (r *scriptRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// eventually は上限つきの条件待ち（固定の待ち時間に頼らない）。
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("条件が満たされない: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

type rig struct {
	c      *Collector
	run    *scriptRunner
	clk    *clock.Fake
	cancel context.CancelFunc
	wait   func()
}

func startRig(t *testing.T, usage, omp bool) *rig {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	run := &scriptRunner{next: map[Command]result{}}
	c := New(Options{
		Runner: run, Clock: clk, UsageCheck: usage, UsageInterval: 5 * time.Minute, OmpCheck: omp, OmpInterval: 24 * time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{c: c, run: run, clk: clk, cancel: cancel, wait: c.Start(ctx)}
	t.Cleanup(func() { cancel(); r.wait() })
	return r
}

// advance は待機（After）が入るのを待ってから時計を進める。
func (r *rig) advance(t *testing.T, waiters int, d time.Duration) {
	t.Helper()
	eventually(t, "待機に入る", func() bool { return r.clk.Waiters() >= waiters })
	r.clk.Advance(d)
}

func TestCollectorPendingAndDisabled(t *testing.T) {
	r := startRig(t, false, false)
	if r.c.Usage().Status != StatusDisabled || r.c.Omp().Status != OmpDisabled {
		t.Fatalf("止めたものは disabled: %+v %+v", r.c.Usage(), r.c.Omp())
	}
	r.clk.Advance(48 * time.Hour)
	r.cancel()
	r.wait()
	if r.run.count() != 0 {
		t.Fatal("止めたコマンドは実行しない")
	}
	r = startRig(t, true, true)
	if u := r.c.Usage(); u.Status != StatusPending || len(u.Claude) != 0 || r.c.Omp().Status != OmpPending {
		t.Fatalf("起動直後は pending: %+v %+v", u, r.c.Omp())
	}
}

// usage は起動 10 秒後・以後は前の実行が終わってから間隔ごと。成功が続いたあと失敗すると stale、前の成功分が残る。
func TestCollectorUsageStale(t *testing.T) {
	r := startRig(t, true, false)
	r.run.set(CmdUsage, result{out: readFixture(t, "usage_ok.json")})
	r.advance(t, 1, 9*time.Second)
	if r.run.count() != 0 {
		t.Fatal("10 秒より前に実行した")
	}
	r.clk.Advance(time.Second)
	eventually(t, "最初の取得", func() bool { return r.c.Usage().Status == StatusOK })
	first := r.c.Usage()
	if len(first.Claude) != 5 || first.Reason != "" {
		t.Fatalf("first = %+v", first)
	}
	r.run.set(CmdUsage, result{reason: ReasonTimeout})
	r.advance(t, 1, 5*time.Minute)
	eventually(t, "stale", func() bool { return r.c.Usage().Status == StatusStale })
	if u := r.c.Usage(); u.Reason != ReasonTimeout || len(u.Claude) != 5 || !u.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("stale は前の成功分を残す: %+v", u)
	}
	// 作って渡した値は、収集器が結果を更新しても変わらない（FR-REP-7・10）。
	if first.Status != StatusOK || first.Reason != "" {
		t.Fatalf("渡した値が変わった: %+v", first)
	}
	// 壊れた JSON は parse。成功すれば ok に戻る。
	r.run.set(CmdUsage, result{out: []byte("{")})
	r.advance(t, 1, 5*time.Minute)
	eventually(t, "parse", func() bool { return r.c.Usage().Reason == ReasonParse })
	r.run.set(CmdUsage, result{out: readFixture(t, "usage_ok.json")})
	r.advance(t, 1, 5*time.Minute)
	eventually(t, "回復", func() bool { return r.c.Usage().Status == StatusOK })
	for _, c := range r.run.calls {
		if c != CmdUsage {
			t.Fatalf("使用状況の収集が別のコマンドを実行した: %v", c)
		}
	}
}

// 成功が一度も無ければ unavailable（reason つき、アカウントは空）。not_found も同じ。
func TestCollectorUsageUnavailable(t *testing.T) {
	r := startRig(t, true, false)
	r.run.set(CmdUsage, result{reason: ReasonNotFound})
	r.advance(t, 1, UsageFirstDelay)
	eventually(t, "unavailable", func() bool { return r.c.Usage().Status == StatusUnavailable })
	if u := r.c.Usage(); u.Reason != ReasonNotFound || u.Claude == nil || len(u.Claude) != 0 || !u.FetchedAt.IsZero() {
		t.Fatalf("%+v", u)
	}
}

// omp は起動 90 秒後に 1 回、間隔は update_check_interval。実行するのは update --check だけ。
func TestCollectorOmp(t *testing.T) {
	r := startRig(t, false, true)
	r.run.set(CmdUpdateCheck, result{out: readFixture(t, "omp_update_available.txt")})
	r.advance(t, 1, 89*time.Second)
	if r.run.count() != 0 {
		t.Fatal("90 秒より前に実行した")
	}
	r.clk.Advance(time.Second)
	eventually(t, "update_available", func() bool { return r.c.Omp().Status == OmpUpdateAvailable })
	if o := r.c.Omp(); o.Current != "18.14.2" || o.Latest != "18.15.0" || o.Reason != "" || !o.CheckedAt.Equal(r.clk.Now()) {
		t.Fatalf("%+v", o)
	}
	r.run.set(CmdUpdateCheck, result{out: []byte("garbage")})
	r.advance(t, 1, 24*time.Hour)
	eventually(t, "unknown", func() bool { return r.c.Omp().Status == OmpUnknown })
	if o := r.c.Omp(); o.Reason != ReasonParse || o.Current != "" || o.Latest != "" {
		t.Fatalf("取れないとき: %+v", o)
	}
	for _, c := range r.run.calls {
		if c != CmdUpdateCheck {
			t.Fatalf("別のコマンドを実行した: %v", c)
		}
	}
}
