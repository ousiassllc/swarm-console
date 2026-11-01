package selfupdatetests

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

// 入れ替え → 起動失敗 3 回 → 戻し → 同じ版は blocked、より新しい版は試す、を通しで確かめる（FR-UPD-12・15・17）。
func TestSwapThenRollbackThenBlocked(t *testing.T) {
	g := newRig(t)
	g.serve(okRelease(newVersion, newContent))
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
	// 新バイナリ（v0.1.5）の 1・2 回目の起動は試運転、3 回目で戻す。
	for boot := 1; boot <= 2; boot++ {
		d := selfupdate.Probation(g.dir, newVersion, g.clk, slog.New(slog.DiscardHandler))
		if d.Trial == nil || d.Trial.From != curVersion {
			t.Fatalf("起動 %d: %+v", boot, d)
		}
	}
	if d := selfupdate.Probation(g.dir, newVersion, g.clk, slog.New(slog.DiscardHandler)); !d.Rollback {
		t.Fatalf("3 回目: %+v", d)
	}
	if g.read(binName) != oldContent || g.exists(oldName) || g.exists(pendingName) {
		t.Fatalf("戻っていません: %v", g.files())
	}
	// 旧バイナリ（v0.1.4）が次の確認で同じ版を見つけても入れ替えない。
	u, err := selfupdate.New(g.opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	g.u = u
	if err := g.apply(newVersion); err == nil || g.u.Status().State != selfupdate.StateBlocked || g.u.Status().Version != newVersion {
		t.Fatalf("blocked でない: err=%v apply=%+v", err, g.u.Status())
	}
	// より新しい版は試す。
	g.serve(okRelease("v0.1.6", "NEWER"))
	if err := g.apply("v0.1.6"); err != nil {
		t.Fatal(err)
	}
	if g.read(binName) != "NEWER" {
		t.Error("より新しい版が入れ替わっていません")
	}
	// 運用者が .update-failed を消せば、同じ版を再び試せる。
	if err := os.Remove(filepath.Join(g.dir, failedName)); err != nil {
		t.Fatal(err)
	}
}

// 試運転中の selfupdate.Updater は apply = probation（from 付き）で始まり、確定で idle に戻る。
func TestUpdaterStartsInProbation(t *testing.T) {
	g := newRig(t)
	u, err := selfupdate.New(g.opts, &selfupdate.Trial{From: "v0.1.3", To: curVersion})
	if err != nil {
		t.Fatal(err)
	}
	if a := u.Status(); a.State != selfupdate.StateProbation || a.From != "v0.1.3" {
		t.Fatalf("apply = %+v", a)
	}
	u.Reset() // probation は Reset で変わらない
	if u.Status().State != selfupdate.StateProbation {
		t.Fatal("Reset が probation を消しました")
	}
	u.Confirmed()
	if a := u.Status(); a.State != selfupdate.StateIdle || a.From != "" {
		t.Fatalf("確定後 = %+v", a)
	}
}

func TestNewRequiresExitAndHome(t *testing.T) {
	if _, err := selfupdate.New(Options{Home: t.TempDir()}, nil); err == nil {
		t.Error("RequestExit なしで作れてしまいます")
	}
	if _, err := selfupdate.New(Options{RequestExit: func() {}}, nil); err == nil {
		t.Error("Home なしで作れてしまいます")
	}
}
