package pane

import (
	"context"
	"sync"

	"github.com/ousiassllc/swarm-console/internal/host"
	"github.com/ousiassllc/swarm-console/internal/tmux"
)

// captureConcurrency はペイン単位の取得の同時数（コンポーネント設計 §5.1）。
const captureConcurrency = 8

// build は tmux・/proc・アダプタから新しい Snapshot を作る。同時に 1 つしか走らない（run が直列化する）。
func (r *SnapshotRegistry) build(ctx context.Context) (*Snapshot, error) {
	now := r.clk.Now()
	cfg := r.deps.Cfg
	snap := &Snapshot{At: now, Panes: []PaneState{}, Layout: Layout{Cols: []int{}}}
	snap.Usage, snap.Omp, snap.Update = r.deps.values()
	snap.Auth = r.deps.authStatuses(now)
	if st, err := r.deps.Host.Status(ctx, cfg.DiskMounts); err != nil {
		r.log.Warn("ホストの状態を読めませんでした", "err", err)
		// CPU・スワップはゼロ値が「取得できず」。ディスクは設定したマウントを残して取得できずにする（FR-REP-11）。
		for _, m := range cfg.DiskMounts {
			snap.Host.Disk = append(snap.Host.Disk, host.Disk{Mount: m})
		}
	} else {
		snap.Host = st
		if st.MemErr != nil {
			// MemTotalGB・MemFreeGB は 0 のまま使う（P-95）。panes_status の mem_free_gb には取得できずの印がない。
			r.log.Warn("メモリを読めませんでした", "err", st.MemErr)
		}
	}
	r.checkVersionOnce(ctx)

	infos, err := r.deps.Tmux.ListPanes(ctx, cfg.Sessions)
	if err != nil {
		r.log.Warn("tmux のペイン一覧を読めませんでした", "err", err)
		snap.Warnings = append(snap.Warnings, Warning{Code: WarnTmuxUnavailable, Message: "tmux に接続できません"})
		r.built = true
		return snap, nil
	}
	r.reconcileServer(ctx)
	infos = SortPanes(infos, cfg)
	snap.TmuxSession = len(infos) > 0
	snap.Warnings = append(snap.Warnings, missingSessions(infos, cfg.Sessions)...)
	if len(infos) == 0 {
		// 一覧が空のときは一時的な失敗と区別できないので、消えた判定・並びの比較には使わない。
		r.built = true
		return snap, nil
	}

	names, locals := NamePositions(infos, cfg), NameLocalPositions(infos, cfg)
	obs := r.observeAll(ctx, infos)
	sums := r.deps.Notes.Summaries()
	snap.Panes = make([]PaneState, len(infos))
	for i, p := range infos {
		o := obs[i]
		changedAt, hash, state, elapsed := r.track(p.ID, o, now)
		act, trunc := TruncateRunes(oneLine(o.activity), activityMax)
		_, fixed := fixedName(p, cfg)
		ps := PaneState{
			Fixed: fixed,
			ID:    p.ID, Pos: names[p.ID], Local: locals[p.ID], GlobalPos: names[p.ID], Session: p.Session, Window: p.Window, PaneIndex: p.PaneIndex,
			Top: p.Top, Left: p.Left,
			Agent: o.agentName, State: state, Title: p.Title, Cwd: p.CurrentPath,
			Elapsed: elapsed, Activity: act, ActivityTrunc: trunc,
			ChangedAgo: FormatDuration(now.Sub(changedAt)), ScreenHash: hash, ChangedAt: changedAt,
			ReportExists: ReportExists(r.deps.FS, cfg.ReportDir, names[p.ID]),
		}
		if s, ok := sums[p.ID]; ok && !s.IsZero() {
			s := s
			ps.Note = &s
		}
		if !o.readable {
			snap.Warnings = append(snap.Warnings, Warning{Code: WarnPaneUnreadable, Message: "ペイン " + ps.Local + " の画面を読めませんでした", Session: ps.Session})
		}
		snap.Panes[i] = ps
	}
	snap.Layout = ComputeLayout(infos, cfg)
	snap.Warnings = append(snap.Warnings, r.updateLayout(snap.Panes, now)...)
	r.trackVanished(ctx, snap.Panes, now)
	r.built = true
	return snap, nil
}

// observeAll は全ペインを同時 captureConcurrency で観測する。1 ペインの失敗は他を止めない。
func (r *SnapshotRegistry) observeAll(ctx context.Context, infos []tmux.PaneInfo) []observation {
	obs := make([]observation, len(infos))
	sem := make(chan struct{}, captureConcurrency)
	var wg sync.WaitGroup
	for i := range infos {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			obs[i] = r.observe(ctx, infos[i])
		}(i)
	}
	wg.Wait()
	return obs
}

// missingSessions は 1 つもペインが見つからなかった対象セッションの警告を、セッションごとに返す。
func missingSessions(infos []tmux.PaneInfo, sessions []string) []Warning {
	have := map[string]bool{}
	for _, p := range infos {
		have[p.Session] = true
	}
	var out []Warning
	for _, s := range sessions {
		if !have[s] {
			out = append(out, Warning{Code: WarnSessionMissing, Message: "tmux セッションが見つかりません: " + s, Session: s})
		}
	}
	return out
}

// reconcileServer は tmux サーバーの起動時刻を読んでメモ側に渡す（P-15）。サーバーが無い（空）ときは渡さない。
func (r *SnapshotRegistry) reconcileServer(ctx context.Context) {
	start, err := r.deps.Tmux.ServerStart(ctx)
	if err != nil {
		r.log.Warn("tmux サーバーの起動時刻を読めませんでした", "err", err)
		return
	}
	if start == "" {
		return
	}
	if _, err := r.deps.Notes.ReconcileServer(ctx, start); err != nil {
		r.log.Warn("tmux 再起動後のメモの整理に失敗しました", "err", err)
	}
}

// checkVersionOnce は最初の 1 回だけ tmux -V を読み、警告をログへ出す（P-24）。
func (r *SnapshotRegistry) checkVersionOnce(ctx context.Context) {
	if r.versionLog || r.deps.VersionWarn == nil {
		return
	}
	v, err := r.deps.Tmux.Version(ctx)
	if err != nil {
		r.log.Warn("tmux のバージョンを読めませんでした", "err", err)
		return
	}
	r.versionLog = true
	if w := r.deps.VersionWarn(v); w != "" {
		r.log.Warn(w, "tmux_version", v)
	}
}
