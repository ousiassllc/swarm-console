package clock

import (
	"sort"
	"sync"
	"time"
)

// Fake は手動で進める Clock。テスト専用だが、他パッケージのテストから使うため通常のファイルに置く。
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

// NewFake は t を現在時刻とする Fake を返す。
func NewFake(t time.Time) *Fake { return &Fake{now: t} }

// Now は Fake の現在時刻を返す。
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After は Advance で d 以上進んだときに時刻を送るチャネルを返す。d <= 0 なら即座に送る。
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.now
		return ch
	}
	f.waiters = append(f.waiters, fakeWaiter{at: f.now.Add(d), ch: ch})
	return ch
}

// Advance は時刻を d 進め、期限に達した待機者を起こす。
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	sort.Slice(f.waiters, func(i, j int) bool { return f.waiters[i].at.Before(f.waiters[j].at) })
	rest := f.waiters[:0]
	for _, w := range f.waiters {
		if !w.at.After(f.now) {
			w.ch <- f.now
			continue
		}
		rest = append(rest, w)
	}
	f.waiters = rest
}

// Waiters は未発火の After の数を返す。テストが「待機に入った」ことを条件待ちで確かめるために使う。
func (f *Fake) Waiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiters)
}
