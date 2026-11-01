package selfupdatetests

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

const (
	prevOldContent = "PREVIOUS-BINARY\n"
	trialMarker    = `{"from":"v0.1.3","to":"v0.1.4","boots":1}`
)

// trialRig は v0.1.3 → v0.1.4 の入れ替え直後（試運転中。.old とマーカーがある）の Updater を持つ rig を返す。
// fs が non-nil なら入れ替えのファイル操作を差し替える。
func trialRig(t *testing.T, fs selfupdate.FS) *rig {
	t.Helper()
	g := newRig(t, func(o *Options) {
		if fs != nil {
			o.FS = fs
		}
	})
	if err := os.WriteFile(filepath.Join(g.dir, oldName), []byte(prevOldContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, pendingName), []byte(trialMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := selfupdate.New(g.opts, &selfupdate.Trial{From: "v0.1.3", To: curVersion})
	if err != nil {
		t.Fatal(err)
	}
	g.u = u
	return g
}

// 試運転中の Apply は何も確かめず何も書かず ErrProbation を返す。入れ替えが途中で失敗する FS でも、
// .old と試運転のマーカー（1 回目の入れ替えのもの）は変わらない（FR-UPD-12・17。Issue #12）。
func TestApplyDuringProbationDoesNothing(t *testing.T) {
	for _, step := range []string{"", "link", "marker", "rename"} {
		t.Run("fail="+step, func(t *testing.T) {
			var fs selfupdate.FS
			if step != "" {
				fs = failFS{fail: step}
			}
			g := trialRig(t, fs)
			g.serve(okRelease(newVersion, newContent))

			if err := g.apply(newVersion); !errors.Is(err, selfupdate.ErrProbation) {
				t.Fatalf("err = %v, want ErrProbation", err)
			}
			if got := g.read(binName); got != oldContent {
				t.Errorf("本体が変わりました: %q", got)
			}
			if got := g.read(oldName); got != prevOldContent {
				t.Errorf(".old が変わりました: %q", got)
			}
			if got := g.read(pendingName); got != trialMarker {
				t.Errorf("試運転のマーカーが変わりました: %q", got)
			}
			if got, want := g.files(), []string{binName, oldName, pendingName}; !slices.Equal(got, want) {
				t.Errorf("置き場のファイル = %v, want %v", got, want)
			}
			if n := len(g.origin.Requests()); n != 0 {
				t.Errorf("通信しました（%d 件）", n)
			}
			if n := g.exits.Load(); n != 0 {
				t.Errorf("終了を要求しました（%d 回）", n)
			}
			// failed にも blocked にもならず、probation のまま（from も残る）。
			if a := g.u.Status(); a.State != selfupdate.StateProbation || a.From != "v0.1.3" {
				t.Errorf("apply = %+v, want probation（from v0.1.3）", a)
			}
			if g.logs.String() != "" {
				t.Errorf("ログが出ています: %s", g.logs.String())
			}
		})
	}
}

// 試運転中は、プラットフォーム・置き場・新しさの条件にも触れない（failed・blocked に上書きしない）。
func TestApplyDuringProbationSkipsPrecheck(t *testing.T) {
	g := trialRig(t, nil)
	g.opts.GOOS = "darwin"
	u, err := selfupdate.New(g.opts, &selfupdate.Trial{From: "v0.1.3", To: curVersion})
	if err != nil {
		t.Fatal(err)
	}
	g.u = u
	if err := g.apply(newVersion); !errors.Is(err, selfupdate.ErrProbation) {
		t.Fatalf("err = %v, want ErrProbation", err)
	}
	if a := g.u.Status(); a.State != selfupdate.StateProbation {
		t.Errorf("apply = %+v, want probation", a)
	}
	if err := g.apply("not-a-version"); !errors.Is(err, selfupdate.ErrInvalidVersion) {
		t.Errorf("不正な版の err = %v, want ErrInvalidVersion", err)
	}
}

// 試運転が確定（probation → idle）すれば、次の周期の Apply は入れ替える。
func TestApplyAfterProbationConfirmed(t *testing.T) {
	g := trialRig(t, nil)
	g.serve(okRelease(newVersion, newContent))
	if err := g.apply(newVersion); !errors.Is(err, selfupdate.ErrProbation) {
		t.Fatalf("err = %v, want ErrProbation", err)
	}
	// 確定（Trial.Run がマーカーを消し、Confirmed を呼ぶ）。
	if err := os.Remove(filepath.Join(g.dir, pendingName)); err != nil {
		t.Fatal(err)
	}
	g.u.Confirmed()
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
	if g.read(binName) != newContent || g.read(oldName) != oldContent || g.exits.Load() != 1 {
		t.Errorf("確定後に入れ替わっていません: %v", g.files())
	}
}
