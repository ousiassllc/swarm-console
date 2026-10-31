package integration

import (
	"slices"
	"sync"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

func callBoth(a, b *harness.Client, tool string, argsA, argsB map[string]any) (ra, rb harness.Result) {
	var wg sync.WaitGroup
	wg.Add(2)
	run := func(c *harness.Client, args map[string]any, out *harness.Result) {
		defer wg.Done()
		r, err := c.TryCall(tool, args)
		if err != nil {
			c.T.Errorf("%s: %v", tool, err)
		}
		*out = r
	}
	go run(a, argsA, &ra)
	go run(b, argsB, &rb)
	wg.Wait()
	return ra, rb
}

// 同じペインへ 2 つの操作を同時に投げると直列化される: 片方の「貼り付け → Enter」が終わってから、もう片方の貼り付けが始まる。
func TestSamePaneOperationsAreSerialized(t *testing.T) {
	h := harness.Start(t, harness.Options{})
	id := h.NewAgent("omp", "idle")
	h.WaitState(id, "idle")
	ra, rb := callBoth(h.Ops(), h.OpsB(), "send_text", map[string]any{"pane": id, "text": "A の本文"}, map[string]any{"pane": id, "text": "B の本文"})
	for _, r := range []harness.Result{ra, rb} {
		if r.IsError || r.Map(t)["accepted"] != true {
			t.Errorf("結果 = %s", r.Text)
		}
	}
	ev := h.WaitEvents(id, "2 回の貼り付け", func(ev []fakeEvent) bool { return len(ev) >= 3 })
	// 交互に混ざらない（PASTE・PASTE・SUBMIT にならない）。先に着いた操作の Enter が、あとの貼り付けより先。
	if kinds := harness.Kinds(ev); len(kinds) != 3 || kinds[0] != "PASTE" || kinds[1] != "SUBMIT" || kinds[2] != "PASTE" {
		t.Errorf("--log = %v", kinds)
	}
	got := []string{ev[0].Text, ev[2].Text}
	if !slices.Contains(got, "A の本文") || !slices.Contains(got, "B の本文") {
		t.Errorf("貼り付けの本文 = %q", got)
	}
}

// lock_timeout を超えて待つ操作は busy_timeout。先に取った操作は最後まで成功する。別のペインへの操作は待たされない。
func TestBusyTimeout(t *testing.T) {
	h := harness.Start(t, harness.Options{Config: map[string]string{"lock_timeout": `"300ms"`}})
	id := h.NewAgent("omp", "idle")
	other := h.NewAgent("claude", "idle")
	h.WaitState(id, "idle")
	h.WaitState(other, "idle")
	ra, rb := callBoth(h.Ops(), h.OpsB(), "send_text", map[string]any{"pane": id, "text": "A"}, map[string]any{"pane": id, "text": "B"})
	var ok, busy harness.Result
	switch {
	case !ra.IsError && rb.IsError:
		ok, busy = ra, rb
	case ra.IsError && !rb.IsError:
		ok, busy = rb, ra
	default:
		t.Fatalf("片方だけが成功のはず:\nA=%s\nB=%s", ra.Text, rb.Text)
	}
	if ok.Map(t)["accepted"] != true || busy.Code(t) != "busy_timeout" {
		t.Errorf("ok=%s busy=%s", ok.Text, busy.Text)
	}
	if e := busy.Err(t).Error; e.Message != apperr.Message(apperr.BusyTimeout) {
		t.Errorf("message = %q", e.Message)
	}
	if ev := h.WaitEvents(id, "1 回だけ貼られる", func(ev []fakeEvent) bool { return len(ev) >= 2 }); len(ev) != 2 {
		t.Errorf("--log = %+v", ev)
	}
	// 別のペインは待たされない。
	if r := h.Ops().Map("send_text", map[string]any{"pane": other, "text": "C"}); r["accepted"] != true {
		t.Errorf("別のペイン: %v", r)
	}
}
