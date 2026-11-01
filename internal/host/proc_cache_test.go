package host

import (
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

func cacheReader(t *testing.T) (*Reader, *clock.Fake) {
	t.Helper()
	fc := clock.NewFake(testNow)
	r := fakeProc(t)
	r.clock = fc
	addProc(t, r.proc, 10, 1, "fish", "fish")
	addProc(t, r.proc, 11, 10, "agent", "agent")
	addProc(t, r.proc, 20, 1, "fish", "fish")
	return r, fc
}

func TestProcTreeSharesOneScan(t *testing.T) {
	r, _ := cacheReader(t)
	for _, pid := range []int{10, 20, 11, 10} {
		if _, err := r.ProcTree(pid); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.scans.Load(); n != 1 {
		t.Fatalf("走査 %d 回、1 回のはず", n)
	}
}

func TestProcTreeRescansAfterTTL(t *testing.T) {
	r, fc := cacheReader(t)
	if _, err := r.ProcTree(10); err != nil {
		t.Fatal(err)
	}
	addProc(t, r.proc, 12, 10, "new", "new")
	fc.Advance(procTTL - time.Millisecond)
	if got, _ := r.ProcTree(10); len(got) != 2 || r.scans.Load() != 1 {
		t.Fatalf("TTL 内は古い表: %+v scans=%d", got, r.scans.Load())
	}
	fc.Advance(time.Millisecond)
	if got, _ := r.ProcTree(10); len(got) != 3 || r.scans.Load() != 2 {
		t.Fatalf("TTL 後は再走査: %+v scans=%d", got, r.scans.Load())
	}
}

func TestProcTreeForcesRescanForUnknownPID(t *testing.T) {
	r, _ := cacheReader(t)
	if _, err := r.ProcTree(10); err != nil {
		t.Fatal(err)
	}
	addProc(t, r.proc, 30, 1, "fish", "fish") // 表を作ったあとに現れたペインのシェル
	got, err := r.ProcTree(30)
	if err != nil || len(got) != 1 || r.scans.Load() != 2 {
		t.Fatalf("強制再走査で見つかるはず: %v %+v scans=%d", err, got, r.scans.Load())
	}
	// 本当に無い pid は 1 回の強制再走査だけで諦める。
	if _, err := r.ProcTree(999); err == nil || r.scans.Load() != 3 {
		t.Fatalf("scans=%d err=%v", r.scans.Load(), err)
	}
}

func TestProcTreeConcurrent(t *testing.T) {
	r, _ := cacheReader(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			if got, err := r.ProcTree(pid); err != nil || got[0].PID != pid {
				t.Errorf("pid %d: %v %+v", pid, err, got)
			}
		}([]int{10, 20}[i%2])
	}
	wg.Wait()
	if n := r.scans.Load(); n != 1 {
		t.Fatalf("同時呼び出しでも走査は 1 回: %d", n)
	}
}
