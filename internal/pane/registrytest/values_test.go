package registrytest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
	"github.com/ousiassllc/swarm-console/internal/reportvalues"
)

// fakeValues は serve が持つ最後の結果。set で差し替える。
type fakeValues struct {
	mu    sync.Mutex
	usage reportvalues.Usage
	omp   reportvalues.Omp
	upd   pane.UpdateInfo
}

func (f *fakeValues) Usage() reportvalues.Usage { f.mu.Lock(); defer f.mu.Unlock(); return f.usage }
func (f *fakeValues) Omp() reportvalues.Omp     { f.mu.Lock(); defer f.mu.Unlock(); return f.omp }
func (f *fakeValues) UpdateInfo() pane.UpdateInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.upd
}

// スナップショットは、作るときの最後の結果を持つ。収集器が結果を更新しても、作ったスナップショットは変わらない（FR-REP-7・10）。
// ログイン期限は agent.Builtin の順に、作るときに 1 回読んで入れる（FR-REP-16）。
func TestSnapshotCarriesValuesAtBuildTime(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	v := &fakeValues{
		usage: reportvalues.Usage{Status: reportvalues.StatusOK, Claude: []reportvalues.Account{{Name: "acct-a"}}},
		omp:   reportvalues.Omp{Status: reportvalues.OmpUpToDate, Current: "1.0.0", Latest: "1.0.0"},
		upd:   pane.UpdateInfo{Status: "up_to_date", Current: "v0.1.5", Apply: pane.ApplyInfo{State: "idle"}},
	}
	r, err := pane.NewRegistry(pane.Deps{
		Cfg: env.Cfg, Tmux: env.Tmux, Host: env.Host, Adapters: env.Adapters, Notes: env.Notes, Clock: env.Clock, FS: env.FS, Values: v,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := mustRefresh(t, r)
	v.mu.Lock()
	v.usage = reportvalues.Usage{Status: reportvalues.StatusStale, Reason: reportvalues.ReasonTimeout}
	v.omp = reportvalues.Omp{Status: reportvalues.OmpUnknown, Reason: reportvalues.ReasonParse}
	v.upd.Status = "unknown"
	v.mu.Unlock()
	if first.Usage.Status != reportvalues.StatusOK || len(first.Usage.Claude) != 1 || first.Omp.Status != reportvalues.OmpUpToDate || first.Update.Status != "up_to_date" {
		t.Fatalf("作ったスナップショットが変わった: %+v %+v %+v", first.Usage, first.Omp, first.Update)
	}
	second := mustRefresh(t, r)
	if second.Usage.Status != reportvalues.StatusStale || second.Omp.Reason != reportvalues.ReasonParse || second.Update.Status != "unknown" {
		t.Fatalf("次のスナップショットは最後の結果: %+v", second)
	}
	var agents []string
	for _, a := range first.Auth {
		agents = append(agents, a.Agent)
	}
	if len(agents) != 3 || agents[0] != "omp" || agents[1] != "claude" || agents[2] != "codex" {
		t.Fatalf("auth の並び: %v", agents)
	}
}

// Values が無ければ pending（起動直後と同じ）。スナップショットの取得は CPU を更新しない（サンプラーだけが SampleCPU を呼ぶ）。
func TestSnapshotWithoutValuesIsPendingAndDoesNotSampleCPU(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	r := newTestRegistry(t, env)
	s := mustRefresh(t, r)
	if s.Usage.Status != reportvalues.StatusPending || s.Omp.Status != reportvalues.OmpPending || s.Update.Status != "pending" {
		t.Fatalf("%+v %+v %+v", s.Usage, s.Omp, s.Update)
	}
	if n := env.Host.Samples(); n != 0 {
		t.Fatalf("読み取りの取得が CPU をサンプルした（%d 回）", n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for env.Host.Samples() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("サンプラーが CPU をサンプルしない")
		}
		time.Sleep(time.Millisecond) // 上限つきのポーリング
	}
	cancel()
	<-done
}

// ホストの状態を読めなかったとき、ディスクは設定したマウントを残して「取得できず」にする（空の一覧にしない。FR-REP-11）。
func TestSnapshotKeepsDiskMountsWhenHostUnreadable(t *testing.T) {
	env := panetest.NewEnv()
	twoRows(env)
	env.Cfg.DiskMounts = []string{"/", "/mnt/nvme"}
	env.Host.StatusErr = errors.New("読めない")
	s := mustRefresh(t, newTestRegistry(t, env))
	if len(s.Host.Disk) != 2 || s.Host.Disk[0].Mount != "/" || s.Host.Disk[1].Mount != "/mnt/nvme" || s.Host.Disk[0].Available || s.Host.Disk[1].Available {
		t.Fatalf("disk = %+v", s.Host.Disk)
	}
	if s.Host.CPU.Available || s.Host.Swap.Available {
		t.Fatalf("CPU・スワップも取得できず: %+v", s.Host)
	}
}
