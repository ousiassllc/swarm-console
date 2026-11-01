package updatecheck_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/clock"
	"github.com/ousiassllc/swarm-console/internal/updatecheck"
	"github.com/ousiassllc/swarm-console/internal/updatecheck/updatechecktest"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// eventually は条件が成り立つまで、上限つきで繰り返し確かめる（固定時間の待ちに頼らない）。
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("条件が成り立ちません: %s", what)
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング間隔
	}
}

// script は呼ばれるたびに次の応答を返す。使い切ったら最後の応答を返す。
type script struct {
	mu    sync.Mutex
	steps []http.HandlerFunc
	n     int
}

func (s *script) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	i := s.n
	s.n++
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	h := s.steps[i]
	s.mu.Unlock()
	h(w, r)
}

func (s *script) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

type rig struct {
	clk    *clock.Fake
	logs   *lockedBuf
	script *script
	runner *updatecheck.Runner
	done   <-chan struct{}
	cancel context.CancelFunc
}

func startRunner(t *testing.T, current string, interval time.Duration, onResult func(context.Context, updatecheck.Result), steps ...http.HandlerFunc) *rig {
	t.Helper()
	o := updatechecktest.New(t)
	sc := &script{steps: steps}
	o.HandleFunc("api.github.com", sc.handler)
	clk := clock.NewFake(t0)
	logs := &lockedBuf{}
	r := updatecheck.NewRunner(updatecheck.RunnerOptions{
		Checker: updatecheck.New(updatecheck.Options{Current: current, Transport: o.Transport(), Timeout: 5 * time.Second}),
		Current: current, Interval: interval, Clock: clk, Logger: slog.New(slog.NewJSONHandler(logs, nil)), OnResult: onResult,
	})
	ctx, cancel := context.WithCancel(context.Background())
	g := &rig{clk: clk, logs: logs, script: sc, runner: r, cancel: cancel}
	g.done = r.Start(ctx)
	t.Cleanup(func() {
		cancel()
		<-g.done
	})
	eventually(t, "最初の待ちに入る", func() bool { return clk.Waiters() == 1 })
	return g
}

// 最初の確認は起動の 1 分後、以後は interval ごと。確認前は pending（FR-UPD-7・9）。
func TestRunnerSchedule(t *testing.T) {
	g := startRunner(t, "v0.1.4", 6*time.Hour, nil, release("v0.1.4"))
	if s := g.runner.Snapshot(); s.Status != updatecheck.Pending || s.Current != "v0.1.4" || !s.CheckedAt.IsZero() {
		t.Fatalf("確認前 = %+v", s)
	}
	g.clk.Advance(59 * time.Second)
	if n := g.script.calls(); n != 0 {
		t.Fatalf("1 分前に通信しました（%d 回）", n)
	}
	g.clk.Advance(time.Second)
	eventually(t, "最初の確認", func() bool { return g.runner.Snapshot().Status == updatecheck.UpToDate })
	s := g.runner.Snapshot()
	if s.Latest != "v0.1.4" || !s.CheckedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("結果 = %+v", s)
	}
	eventually(t, "次の周期の待ちに入る", func() bool { return g.clk.Waiters() == 1 })
	g.clk.Advance(6*time.Hour - time.Second)
	if n := g.script.calls(); n != 1 {
		t.Fatalf("周期の前に通信しました（%d 回）", n)
	}
	g.clk.Advance(time.Second)
	eventually(t, "2 回目の確認", func() bool { return g.script.calls() == 2 })
}

// 失敗しても止まらず、次の周期で再び確認する。前回成功した latest は残り、失敗の reason だけ変わる（FR-UPD-8）。
func TestRunnerSurvivesFailuresAndKeepsLatest(t *testing.T) {
	g := startRunner(t, "v0.1.4", time.Hour, nil,
		release("v0.1.5"), status(500), status(429), status(500), release("v0.1.5"))
	step := func(wantCalls int) updatecheck.Snapshot {
		t.Helper()
		g.clk.Advance(time.Hour)
		eventually(t, "確認が終わる", func() bool { return g.script.calls() == wantCalls && g.clk.Waiters() == 1 })
		return g.runner.Snapshot()
	}
	g.clk.Advance(time.Minute)
	eventually(t, "1 回目", func() bool { return g.script.calls() == 1 && g.clk.Waiters() == 1 })
	if s := g.runner.Snapshot(); s.Status != updatecheck.UpdateAvailable || s.Latest != "v0.1.5" {
		t.Fatalf("1 回目 = %+v", s)
	}
	if s := step(2); s.Status != updatecheck.Unknown || s.Reason != updatecheck.ReasonNetwork || s.Latest != "v0.1.5" {
		t.Fatalf("500 のあと = %+v", s)
	}
	if s := step(3); s.Reason != updatecheck.ReasonRateLimit {
		t.Fatalf("429 のあと = %+v", s)
	}
	step(4)
	if s := step(5); s.Status != updatecheck.UpdateAvailable || s.Reason != "" {
		t.Fatalf("回復後 = %+v", s)
	}
	logs := g.logs.String()
	// 新しい版は初めて見たとき 1 回、失敗は reason が変わったとき（network → rate_limit → network）だけ。
	if n := strings.Count(logs, "新しい版があります"); n != 1 {
		t.Errorf("新しい版のログ %d 回, want 1\n%s", n, logs)
	}
	if n := strings.Count(logs, "確認できませんでした"); n != 3 {
		t.Errorf("失敗のログ %d 回, want 3（network, rate_limit, network）\n%s", n, logs)
	}
	for _, leak := range []string{"SECRET", "evil.example", "api.github.com"} {
		if strings.Contains(logs, leak) {
			t.Errorf("ログに %q が出ています:\n%s", leak, logs)
		}
	}
}

// 同じ reason が続くときは警告を繰り返さない。
func TestRunnerRepeatedFailureWarnsOnce(t *testing.T) {
	g := startRunner(t, "v0.1.4", time.Hour, nil, status(503))
	g.clk.Advance(time.Minute)
	eventually(t, "1 回目", func() bool { return g.script.calls() == 1 && g.clk.Waiters() == 1 })
	for i := 2; i <= 3; i++ {
		g.clk.Advance(time.Hour)
		eventually(t, "続きの確認", func() bool { return g.script.calls() == i && g.clk.Waiters() == 1 })
	}
	if n := strings.Count(g.logs.String(), "確認できませんでした"); n != 1 {
		t.Errorf("警告 %d 回, want 1\n%s", n, g.logs.String())
	}
}

// OnResult は確認のたびに結果つきで呼ばれる。止まった確認はそのあとの OnResult を待たせない（ctx で終わる）。
func TestRunnerOnResult(t *testing.T) {
	var got []updatecheck.Result
	var mu sync.Mutex
	g := startRunner(t, "v0.1.4", time.Hour, func(_ context.Context, r updatecheck.Result) {
		mu.Lock()
		got = append(got, r)
		mu.Unlock()
	}, release("v0.1.5"))
	g.clk.Advance(time.Minute)
	eventually(t, "OnResult", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })
	if got[0].Status != updatecheck.UpdateAvailable || got[0].Latest != "v0.1.5" {
		t.Errorf("OnResult = %+v", got[0])
	}
}

// 応答が返らない確認も、SIGTERM（ctx のキャンセル）で中断して goroutine が終わる（FR-UPD-8）。
func TestRunnerStopsDuringHungCheck(t *testing.T) {
	var started atomic.Bool
	g := startRunner(t, "v0.1.4", time.Hour, nil, func(_ http.ResponseWriter, r *http.Request) {
		started.Store(true)
		<-r.Context().Done()
	})
	g.clk.Advance(time.Minute)
	eventually(t, "確認が始まる", started.Load)
	g.cancel()
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		t.Fatal("ctx を止めても確認の goroutine が終わりません")
	}
	if s := g.runner.Snapshot(); s.Status != updatecheck.Pending {
		t.Errorf("中断した確認の結果が残っています: %+v", s)
	}
}

// 待ちの途中で止めても goroutine が終わる。
func TestRunnerStopsWhileWaiting(t *testing.T) {
	g := startRunner(t, "v0.1.4", time.Hour, nil, release("v0.1.4"))
	g.cancel()
	select {
	case <-g.done:
	case <-time.After(10 * time.Second):
		t.Fatal("待ちの途中で止まりません")
	}
	if n := g.script.calls(); n != 0 {
		t.Errorf("止めたのに通信しました（%d 回）", n)
	}
}

// update_check = false のときは通信も待ちもなく、disabled を返す。
func TestRunnerDisabled(t *testing.T) {
	o := updatechecktest.New(t)
	clk := clock.NewFake(t0)
	r := updatecheck.NewRunner(updatecheck.RunnerOptions{
		Checker: updatecheck.New(updatecheck.Options{Current: "v0.1.4", Transport: o.Transport()}),
		Current: "v0.1.4", Disabled: true, Interval: time.Hour, Clock: clk,
	})
	select {
	case <-r.Start(context.Background()):
	case <-time.After(10 * time.Second):
		t.Fatal("無効の Runner が終わりません")
	}
	if s := r.Snapshot(); s.Status != updatecheck.Disabled || s.Current != "v0.1.4" {
		t.Errorf("Snapshot = %+v", s)
	}
	if clk.Waiters() != 0 || len(o.Requests()) != 0 {
		t.Error("無効なのに待ち・通信があります")
	}
}

// dev ビルドは通信せず unknown(dev_build) を返す。
func TestRunnerDevBuild(t *testing.T) {
	g := startRunner(t, "dev", time.Hour, nil, release("v9.9.9"))
	g.clk.Advance(time.Minute)
	eventually(t, "結果", func() bool { return g.runner.Snapshot().Status == updatecheck.Unknown })
	if s := g.runner.Snapshot(); s.Reason != updatecheck.ReasonDevBuild || s.Latest != "" {
		t.Errorf("Snapshot = %+v", s)
	}
	if n := g.script.calls(); n != 0 {
		t.Errorf("dev ビルドが通信しました（%d 回）", n)
	}
}
