package selfupdatetests

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

// 入れ替えない条件（FR-UPD-12）：置き場・プラットフォーム。通信もしない。
func TestApplyPreconditions(t *testing.T) {
	notManagedAt := func(path string) func(*Options) {
		return func(o *Options) { o.Executable = func() (string, error) { return path, nil } }
	}
	tests := []struct {
		name   string
		mutate func(*testing.T, *rig) func(*Options)
		want   Reason
	}{
		{"linux 以外", func(*testing.T, *rig) func(*Options) { return func(o *Options) { o.GOOS = "darwin" } }, selfupdate.ReasonUnsupportedPlatform},
		{"amd64 以外", func(*testing.T, *rig) func(*Options) { return func(o *Options) { o.GOARCH = "arm64" } }, selfupdate.ReasonUnsupportedPlatform},
		{"置き場が違う", func(t *testing.T, _ *rig) func(*Options) {
			other := filepath.Join(t.TempDir(), binName)
			_ = os.WriteFile(other, []byte(oldContent), 0o755) //nolint:gosec // テスト用
			return notManagedAt(other)
		}, selfupdate.ReasonNotManaged},
		{"置き場の名前が違う", func(_ *testing.T, g *rig) func(*Options) {
			p := filepath.Join(g.dir, "swarm-console.copy")
			_ = os.WriteFile(p, []byte(oldContent), 0o755) //nolint:gosec // テスト用
			return notManagedAt(p)
		}, selfupdate.ReasonNotManaged},
		{"実行ファイルのパスが取れない", func(*testing.T, *rig) func(*Options) {
			return func(o *Options) { o.Executable = func() (string, error) { return "", errors.New("no") } }
		}, selfupdate.ReasonNotManaged},
		{"本体が無い", func(_ *testing.T, g *rig) func(*Options) {
			_ = os.Remove(g.exe)
			return func(*Options) {}
		}, selfupdate.ReasonNotManaged},
		{"本体がシンボリックリンク", func(t *testing.T, g *rig) func(*Options) {
			target := filepath.Join(t.TempDir(), "target")
			_ = os.WriteFile(target, []byte(oldContent), 0o755) //nolint:gosec // テスト用
			_ = os.Remove(g.exe)
			if err := os.Symlink(target, g.exe); err != nil {
				t.Fatal(err)
			}
			return func(*Options) {}
		}, selfupdate.ReasonNotManaged},
		{"本体がディレクトリ", func(t *testing.T, g *rig) func(*Options) {
			_ = os.Remove(g.exe)
			if err := os.Mkdir(g.exe, 0o700); err != nil {
				t.Fatal(err)
			}
			return func(*Options) {}
		}, selfupdate.ReasonNotManaged},
		{"置き場に書けない", func(t *testing.T, g *rig) func(*Options) {
			if err := os.Chmod(g.dir, 0o500); err != nil { //nolint:gosec // 書けない置き場を作る
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(g.dir, 0o700) }) //nolint:gosec // 後始末で権限を戻す
			return func(*Options) {}
		}, selfupdate.ReasonNotManaged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newRig(t)
			g.serve(okRelease(newVersion, newContent))
			mut := tt.mutate(t, g)
			mut(&g.opts)
			u, err := selfupdate.New(g.opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			g.u = u
			wantFailure(t, g.apply(newVersion), tt.want)
			if n := len(g.origin.Requests()); n != 0 {
				t.Errorf("条件を満たさないのに通信しました（%d 件）", n)
			}
			if g.exits.Load() != 0 {
				t.Error("終了を要求しました")
			}
			if entries, _ := os.ReadDir(g.dir); slices.ContainsFunc(entries, func(e os.DirEntry) bool {
				return e.Name() == newName || e.Name() == oldName || e.Name() == pendingName
			}) {
				t.Errorf("置き場にファイルが増えました: %v", entries)
			}
			g.wantApply(selfupdate.StateFailed, tt.want)
		})
	}
}

// ダウングレード・同じ版・dev ビルドは入れ替えない。通信もせず、apply の状況も変えない（FR-UPD-12 (3)）。
func TestApplyNotNewer(t *testing.T) {
	for _, tc := range []struct{ current, latest string }{
		{"v0.1.5", "v0.1.5"}, {"v0.1.5", "v0.1.4"}, {"v0.1.10", "v0.1.9"}, {"dev", "v0.1.5"}, {"v0.1.5-rc.1", "v0.1.4"},
	} {
		t.Run(tc.current+"→"+tc.latest, func(t *testing.T) {
			g := newRig(t, func(o *Options) { o.Current = tc.current })
			g.serve(okRelease(tc.latest, newContent))
			if err := g.apply(tc.latest); !errors.Is(err, selfupdate.ErrNotNewer) {
				t.Fatalf("err = %v", err)
			}
			if len(g.origin.Requests()) != 0 {
				t.Error("通信しました")
			}
			g.assertUntouched()
			g.wantApply(selfupdate.StateIdle, "")
		})
	}
}

func TestApplyRejectsInvalidVersion(t *testing.T) {
	for _, tag := range []string{"", "0.1.5", "v0.1", "v0.1.5-rc.1", "v0.1.5/../x", "v0.1.5\n", "../../evil"} {
		g := newRig(t)
		if err := g.apply(tag); !errors.Is(err, selfupdate.ErrInvalidVersion) {
			t.Errorf("%q: err = %v", tag, err)
		}
		if len(g.origin.Requests()) != 0 {
			t.Errorf("%q: 通信しました", tag)
		}
	}
}

// .update-failed の版以下は入れ替えない（blocked）。それより新しい版は試す（FR-UPD-12 (4)・17）。
func TestApplyBlockedByUpdateFailed(t *testing.T) {
	setup := func(t *testing.T, failedBody string) *rig {
		g := newRig(t, func(o *Options) { o.Current = "v0.1.3" })
		if err := os.WriteFile(filepath.Join(g.dir, failedName), []byte(failedBody), 0o600); err != nil {
			t.Fatal(err)
		}
		return g
	}
	for _, latest := range []string{"v0.1.4", "v0.1.5"} { // 戻した版（v0.1.5）と、それ以下
		t.Run("blocked "+latest, func(t *testing.T) {
			g := setup(t, `{"version":"v0.1.5","at":"2026-11-01T12:30:00+09:00"}`)
			g.serve(okRelease(latest, newContent))
			if err := g.apply(latest); !errors.Is(err, selfupdate.ErrBlocked) {
				t.Fatalf("err = %v", err)
			}
			if a := g.u.Status(); a.State != selfupdate.StateBlocked || a.Version != "v0.1.5" {
				t.Errorf("apply = %+v", a)
			}
			if len(g.origin.Requests()) != 0 {
				t.Error("通信しました")
			}
			if g.read(binName) != oldContent || g.exists(newName) || g.exists(pendingName) {
				t.Error("置き場が変わりました")
			}
		})
	}
	t.Run("より新しい版は試す", func(t *testing.T) {
		g := setup(t, `{"version":"v0.1.5","at":"2026-11-01T12:30:00+09:00"}`)
		g.serve(okRelease("v0.1.6", newContent))
		if err := g.apply("v0.1.6"); err != nil {
			t.Fatal(err)
		}
		if g.read(binName) != newContent {
			t.Error("入れ替わっていません")
		}
	})
	for name, bad := range map[string]string{"壊れた JSON": "{", "版の形が違う": `{"version":"v0.1","at":"x"}`, "未知のキー": `{"version":"v0.1.5","at":"x","z":1}`} {
		t.Run("読めない "+name+" は無いものとして消す", func(t *testing.T) {
			g := setup(t, bad)
			g.serve(okRelease("v0.1.4", newContent))
			if err := g.apply("v0.1.4"); err != nil {
				t.Fatal(err)
			}
			if g.exists(failedName) {
				t.Error("読めない .update-failed が残っています")
			}
		})
	}
}

// SIGTERM（ctx のキャンセル）で通信を中断し、書きかけの .new を消す。失敗として数えない（FR-UPD-8）。
func TestApplyAbortOnCancelCleansUp(t *testing.T) {
	g := newRig(t)
	partial := make(chan struct{})
	g.serve(release{tag: newVersion, sums: body(sumsLine(newVersion, newContent)), binary: func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial-data"))
		w.(http.Flusher).Flush()
		close(partial)
		<-r.Context().Done()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.u.Apply(ctx, newVersion) }()
	<-partial
	eventually(t, ".new に書きかけが入る", func() bool {
		b, err := os.ReadFile(filepath.Join(g.dir, newName))
		return err == nil && strings.HasPrefix(string(b), "partial-data")
	})
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel で Apply が戻りません")
	}
	g.assertUntouched()
	g.wantApply(selfupdate.StateIdle, "")
}

// ダウンロード前に止められたら通信しない。
func TestApplyCanceledBeforeStart(t *testing.T) {
	g := newRig(t)
	g.serve(okRelease(newVersion, newContent))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.u.Apply(ctx, newVersion); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	g.assertUntouched()
}
