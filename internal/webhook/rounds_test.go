package webhook

import (
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

// Issue #93: ペインの位置がずれて最終報告のパスが変わったら、新しいパスで基準を取り直す。
// 別のペインの古い報告（T1.md）を「書かれた」と見間違えない。本物の報告が新しいパスに書かれたら送る。
func TestReportPathChangeMidRoundRebaselines(t *testing.T) {
	const staleT1 = reportDir + "/0_T1.md"
	atT1 := func(h *harness, state agent.State) {
		h.snap(h.paneAt("%3", "0:T1", "T1", state))
	}
	for name, setup := range map[string]func(h *harness){
		"working の間にずれる": func(h *harness) {
			h.observe(agent.StateWorking)
			atT1(h, agent.StateWorking)
			atT1(h, agent.StateIdle)
		},
		"idle で報告を待つ間にずれる": func(h *harness) {
			h.observe(agent.StateWorking)
			h.observe(agent.StateIdle) // T2 の報告はまだ無い
			atT1(h, agent.StateIdle)
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.start()
			h.reportAt(staleT1, 500) // 別のペインの、古い最終報告
			setup(h)
			atT1(h, agent.StateIdle)
			if got := len(h.received()); got != 0 {
				t.Fatalf("古い T1.md で送りました: %d 回", got)
			}

			h.reportAt(staleT1, 2000) // 本物の報告が新しいパスに書かれた
			atT1(h, agent.StateIdle)
			if got := len(h.received()); got != 1 {
				t.Fatalf("POST = %d 回, want 1", got)
			}
			if pl := h.payload(0); pl.ReportPath != staleT1 || pl.Pane != "T1" {
				t.Errorf("payload = %+v", pl)
			}
		})
	}
}
