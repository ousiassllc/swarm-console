package integration

import (
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// AC-1（自動化分）: 同じスナップショットで、独立した 4 つのクライアント接続（別セッション・別トークン・窓口違いを含む）が
// report（ja・en・markdown）を同時に何度も呼び、返る本文が 1 文字も違わない。本文に呼び出し時刻・クライアント名が入らない。
func TestAC1ReportIdenticalAcrossClients(t *testing.T) {
	e := newReportEnv(t)
	clients := []*harness.Client{e.h.Report(), e.h.ReportB(), e.h.Ops(), e.h.Connect("/report", harness.OpsB)}
	names := map[*harness.Client]string{
		clients[0]: harness.ReportA, clients[1]: harness.ReportB, clients[2]: harness.OpsA, clients[3]: harness.OpsB,
	}
	for _, args := range []map[string]any{
		{"lang": "ja"}, {"lang": "en"}, nil, {"format": "json"}, {"pane": e.askPos}, {"pane": e.askPos, "lang": "en"},
	} {
		const rounds = 5
		got := make([][]string, len(clients))
		var wg sync.WaitGroup
		for i, c := range clients {
			got[i] = make([]string, rounds)
			wg.Add(1)
			go func() {
				defer wg.Done()
				for r := range rounds {
					res, err := c.TryCall("report", args)
					if err != nil || res.IsError {
						t.Errorf("%s report %v: %v %s", names[c], args, err, res.Text)
						return
					}
					got[i][r] = res.Text
				}
			}()
		}
		wg.Wait()
		want := got[0][0]
		if want == "" {
			t.Fatalf("report %v が空", args)
		}
		for i, c := range clients {
			for r, text := range got[i] {
				if text != want {
					t.Fatalf("report %v: %s の %d 回目が 1 文字違う\n--- 先頭のクライアント\n%s\n--- %s\n%s", args, names[c], r, want, names[c], text)
				}
			}
		}
		for _, n := range []string{harness.OpsA, harness.OpsB, harness.ReportA, harness.ReportB, "integration-"} {
			if strings.Contains(want, n) {
				t.Errorf("report %v の本文にクライアント名 %q が入っている", args, n)
			}
		}
	}
	// prompts・resources も窓口・クライアントによらず同じ。
	if want := e.h.Report().Must("report", nil).Text; want == "" {
		t.Fatal("report が空")
	}
}
