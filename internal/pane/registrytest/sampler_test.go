package registrytest

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

func TestCurrentCachesWithinTTLAndRefetchesAfter(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	ctx := context.Background()
	s1, err := r.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	env.Clock.Advance(time.Second)
	s2, _ := r.Current(ctx)
	if s1 != s2 || env.Tmux.ListCalls() != 1 {
		t.Fatalf("TTL 内はキャッシュ: same=%v calls=%d", s1 == s2, env.Tmux.ListCalls())
	}
	env.Clock.Advance(time.Second) // 合計 2 秒 = TTL 以上
	s3, _ := r.Current(ctx)
	if s3 == s1 || env.Tmux.ListCalls() != 2 || !s3.At.Equal(env.Clock.Now()) {
		t.Fatalf("TTL 超過で再取得: same=%v calls=%d", s3 == s1, env.Tmux.ListCalls())
	}
}

func TestCurrentSingleflight(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	gate := make(chan struct{})
	env.Tmux.OnList(func() { <-gate })

	const n = 16
	got := make([]*pane.Snapshot, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = r.Current(context.Background())
		}()
	}
	panetest.WaitUntil(t, "list-panes が呼ばれる", func() bool { return env.Tmux.ListCalls() >= 1 })
	close(gate)
	wg.Wait()
	if c := env.Tmux.ListCalls(); c != 1 {
		t.Fatalf("同時 %d 呼び出しで list-panes %d 回", n, c)
	}
	for i := range got {
		if got[i] == nil || got[i] != got[0] {
			t.Fatalf("同じスナップショットを受け取っていない: %d", i)
		}
	}
}

func TestFetchHonoursContextCancel(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	gate := make(chan struct{})
	env.Tmux.OnList(func() { <-gate })
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := r.Current(ctx); errc <- err }()
	panetest.WaitUntil(t, "進行中", func() bool { return env.Tmux.ListCalls() == 1 })
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	close(gate)
}

func TestLayoutChangedWarning(t *testing.T) {
	env := panetest.NewEnv()
	ps := twoRows(env)
	r := newTestRegistry(t, env)

	if s := mustRefresh(t, r); len(s.Warnings) != 0 {
		t.Fatalf("起動直後は警告しない: %+v", s.Warnings)
	}
	if s := mustRefresh(t, r); len(s.Warnings) != 0 {
		t.Fatalf("変化なし: %+v", s.Warnings)
	}
	// 追加: 3 段目に新しいペイン。
	added := env.Pane("%9", 109, 40, 0, panetest.ScreenIdle("omp"), []string{"omp"})
	env.Tmux.SetPanes(append(ps, added)...)
	s := mustRefresh(t, r)
	// 3 段になるので位置が R 系に変わる: 追加・位置変更がまとめて出る。
	if codes := warningCodes(s); len(codes) != 1 || codes[0] != pane.WarnLayoutChanged {
		t.Fatalf("warnings = %+v", s.Warnings)
	}
	msg := s.Warnings[0].Message
	for _, sub := range []string{"並びが変わりました: ", "R31 が追加されました", "R11 が T1→R11 に変わりました"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("message %q に %q が無い", msg, sub)
		}
	}
	// TTL 内は出し続ける（検知時刻から 10 分）。
	env.Clock.Advance(9 * time.Minute)
	if s := mustRefresh(t, r); len(s.Warnings) != 1 || s.Warnings[0].Message != msg {
		t.Fatalf("TTL 内: %+v", s.Warnings)
	}
	// 新しい変更（削除）で更新され、TTL が延びる。
	env.Tmux.SetPanes(ps...)
	s = mustRefresh(t, r)
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0].Message, "R31 が削除されました") {
		t.Fatalf("削除: %+v", s.Warnings)
	}
	env.Clock.Advance(9 * time.Minute)
	if s := mustRefresh(t, r); len(s.Warnings) != 1 {
		t.Fatalf("更新後の TTL 内: %+v", s.Warnings)
	}
	env.Clock.Advance(time.Minute)
	if s := mustRefresh(t, r); len(s.Warnings) != 0 {
		t.Fatalf("TTL 経過で消える: %+v", s.Warnings)
	}
}

func TestLayoutChangedRemoval(t *testing.T) {
	env := panetest.NewEnv()
	ps := twoRows(env)
	r := newTestRegistry(t, env)
	mustRefresh(t, r)
	env.Tmux.SetPanes(ps[0], ps[1]) // B1 が消える（段が 1 つになるので T1・T2 のまま）
	s := mustRefresh(t, r)
	if len(s.Warnings) != 1 || s.Warnings[0].Message != "並びが変わりました: B1 が削除されました" {
		t.Fatalf("%+v", s.Warnings)
	}
}

func TestVanishedAfterTwoConsecutiveSnapshots(t *testing.T) {
	env := panetest.NewEnv()
	ps := twoRows(env)
	env.Notes.SetSummary("%2", notes.Summary{From: "agent-b"})
	r := newTestRegistry(t, env)
	mustRefresh(t, r)

	env.Tmux.SetPanes(ps[0], ps[1])
	mustRefresh(t, r)
	if len(env.Notes.Archives()) != 0 {
		t.Fatal("1 回見えないだけで移した")
	}
	// 1 回見えて、また見えない → 連続ではないのでまだ移さない。
	env.Tmux.SetPanes(ps...)
	mustRefresh(t, r)
	env.Tmux.SetPanes(ps[0], ps[1])
	mustRefresh(t, r)
	if len(env.Notes.Archives()) != 0 {
		t.Fatal("連続していないのに移した")
	}
	env.Clock.Advance(time.Minute)
	mustRefresh(t, r)
	got := env.Notes.Archives()
	if len(got) != 1 || got[0].ClosedBy != "vanished" || got[0].Meta.PaneID != "%2" || got[0].Meta.Pos != "B1" ||
		got[0].Meta.Agent != "omp" || got[0].Meta.Title != "t%2" || !got[0].Meta.At.Equal(env.Clock.Now()) {
		t.Fatalf("archives = %+v", got)
	}
	mustRefresh(t, r)
	if len(env.Notes.Archives()) != 1 {
		t.Fatal("同じペインを 2 度移した")
	}
}

func TestReconcileServerEverySnapshot(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	mustRefresh(t, r)
	env.Tmux.SetServerStart("2000")
	mustRefresh(t, r)
	env.Tmux.SetServerStart("") // サーバーが無い（空）ときは渡さない
	mustRefresh(t, r)
	if got := env.Notes.Reconciles(); strings.Join(got, ",") != "1000,2000" {
		t.Fatalf("reconciles = %v", got)
	}
}

type recordHandler struct {
	mu   *sync.Mutex
	msgs *[]string
}

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.msgs = append(*h.msgs, r.Message)
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func TestVersionWarnOnce(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	var mu sync.Mutex
	var msgs []string
	var seen []string
	r, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: env.FS,
		Logger: slog.New(recordHandler{&mu, &msgs}),
		VersionWarn: func(v string) string {
			seen = append(seen, v)
			return "古い tmux です"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustRefresh(t, r)
	mustRefresh(t, r)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "tmux 3.4" || len(msgs) != 1 || msgs[0] != "古い tmux です" {
		t.Fatalf("seen=%v msgs=%v", seen, msgs)
	}
}

// meminfo を読めなかったとき（Status.MemErr）は Snapshot の host をそのまま使い、警告を 1 件残す（P-95）。
// 読めたときは残さない。
func TestSnapshotWarnsWhenMemoryUnreadable(t *testing.T) {
	for _, memErr := range []error{errors.New("読めない"), nil} {
		env := panetest.NewEnv()
		twoRows(env)
		env.Host.Load = 1.5
		env.Host.MemErr = memErr
		var mu sync.Mutex
		var msgs []string
		r, err := pane.NewRegistry(pane.Deps{
			Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: env.FS,
			Logger: slog.New(recordHandler{&mu, &msgs}),
		})
		if err != nil {
			t.Fatal(err)
		}
		s := mustRefresh(t, r)
		if s.Host.Load[0] != 1.5 || !errors.Is(s.Host.MemErr, memErr) {
			t.Fatalf("host を使うはず: %+v", s.Host)
		}
		mu.Lock()
		warned := strings.Join(msgs, ",") == "メモリを読めませんでした"
		mu.Unlock()
		if warned != (memErr != nil) || (memErr == nil && len(msgs) != 0) {
			t.Fatalf("memErr=%v msgs=%v", memErr, msgs)
		}
	}
}

func TestRunSamplesEveryPollInterval(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	panetest.WaitUntil(t, "最初の取得", func() bool { return env.Tmux.ListCalls() == 1 && env.Clock.Waiters() == 1 })
	env.Clock.Advance(4 * time.Second)
	if env.Tmux.ListCalls() != 1 {
		t.Fatal("poll_interval 前に取得した")
	}
	env.Clock.Advance(time.Second)
	panetest.WaitUntil(t, "2 回目の取得", func() bool { return env.Tmux.ListCalls() == 2 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

func TestNewRegistryRequiresDeps(t *testing.T) {
	if _, err := pane.NewRegistry(pane.Deps{}); err == nil {
		t.Fatal("依存なしで作れてしまう")
	}
}
