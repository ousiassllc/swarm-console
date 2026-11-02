package integration

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

const second = "second"

// grid は pane0 のあるウィンドウを 3 列×2 段に分け、ペイン ID を T1–T3・B1–B3 の順で返す。
func grid(h *harness.H, pane0 string) []string {
	split := func(flag, target string) string {
		return strings.TrimSpace(h.TM.Run("split-window", flag, "-d", "-P", "-F", "#{pane_id}", "-t", target))
	}
	b1 := split("-v", pane0)
	t2 := split("-h", pane0)
	t3 := split("-h", t2)
	b2 := split("-h", b1)
	b3 := split("-h", b2)
	return []string{pane0, t2, t3, b1, b2, b3}
}

// 2 つの tmux セッション（それぞれ 3 列×2 段）。OpsA は main だけ・OpsB は second だけ。
func twoSessionHarness(t *testing.T) (h *harness.H, mainIDs, secondIDs []string) {
	t.Helper()
	h = harness.Start(t, harness.Options{
		Width: 250, Height: 85, ExtraSessions: []string{second},
		TokenSessions: map[string][]string{
			harness.OpsA: {harness.Session}, harness.OpsB: {second}, harness.ReportA: {harness.Session},
		},
	})
	mainIDs, secondIDs = grid(h, h.P0), grid(h, h.PExtra[second])
	waitShells(t, h.Ops(), nil, mainIDs)
	waitShells(t, h.OpsB(), nil, secondIDs)
	return h, mainIDs, secondIDs
}

// waitShells は c から見て、ids のペインがすべて shell になるまで待つ（トークンごとに見えるセッションが違う）。
func waitShells(t *testing.T, c *harness.Client, args map[string]any, ids []string) {
	t.Helper()
	harness.Eventually(t, c.Name+" から ids が shell になる", func() bool {
		shells := map[string]bool{}
		for _, p := range c.Map("panes_status", args)["panes"].([]any) {
			m := p.(map[string]any)
			shells[m["id"].(string)] = m["state"] == "shell"
		}
		for _, id := range ids {
			if !shells[id] {
				return false
			}
		}
		return true
	})
}

func statusOf(t *testing.T, c *harness.Client, args map[string]any) (ids, pos []string, layout map[string]any) {
	t.Helper()
	m := c.Map("panes_status", args)
	for _, p := range m["panes"].([]any) {
		ids = append(ids, p.(map[string]any)["id"].(string))
		pos = append(pos, p.(map[string]any)["pos"].(string))
	}
	return ids, pos, m["layout"].(map[string]any)
}

// 受け入れ 1・2: 互いのセッションを読めも操作もできない。省略時は自分のセッション、位置は T1 形式（接頭辞なし）。
func TestSessionIsolation(t *testing.T) {
	h, mainIDs, secondIDs := twoSessionHarness(t)
	wantPos := []string{"T1", "T2", "T3", "B1", "B2", "B3"}
	for _, c := range []struct {
		client *harness.Client
		ids    []string
	}{{h.Ops(), mainIDs}, {h.OpsB(), secondIDs}} {
		ids, pos, layout := statusOf(t, c.client, nil)
		if !reflect.DeepEqual(pos, wantPos) || !reflect.DeepEqual(layout["cols"], []any{3.0, 3.0}) || layout["rows"] != 2.0 {
			t.Errorf("%s: pos=%v layout=%v", c.client.Name, pos, layout)
		}
		if !slices.Equal(sortedCopy(ids), sortedCopy(c.ids)) {
			t.Errorf("%s のペイン = %v, want %v", c.client.Name, ids, c.ids)
		}
	}
	// 同じ T1 でも、トークンごとに自分のセッションのペイン。
	if got := h.Ops().Map("pane_tail", map[string]any{"pane": "T1"})["pane"]; got != "T1" {
		t.Errorf("pane = %v", got)
	}

	before := h.TM.Capture(secondIDs[0])
	for name, call := range map[string]func() harness.Result{
		"panes_status": func() harness.Result { return h.Ops().Call("panes_status", map[string]any{"session": second}) },
		"pane_tail": func() harness.Result {
			return h.Ops().Call("pane_tail", map[string]any{"session": second, "pane": "T1"})
		},
		"pane_tail ID": func() harness.Result { return h.Ops().Call("pane_tail", map[string]any{"pane": secondIDs[0]}) },
		"send_text": func() harness.Result {
			return h.Ops().Call("send_text", map[string]any{"pane": secondIDs[0], "text": "echo FORBIDDEN-MARK"})
		},
		"dispatch": func() harness.Result {
			return h.Ops().Call("dispatch", map[string]any{"session": second, "pane": "T1", "agent": "omp", "task": "t", "profile": "fix"})
		},
		"report":      func() harness.Result { return h.Ops().Call("report", map[string]any{"session": second}) },
		"host_status": func() harness.Result { return h.Ops().Call("host_status", map[string]any{"session": second}) },
		"逆向き": func() harness.Result {
			return h.OpsB().Call("pane_tail", map[string]any{"session": harness.Session, "pane": "T1"})
		},
		"report 窓口": func() harness.Result { return h.Report().Call("panes_status", map[string]any{"session": second}) },
	} {
		e := call().Err(t)
		if e.Error.Code != "session_forbidden" || e.Error.Message != "このセッションは許可されていません" {
			t.Errorf("%s: %+v", name, e.Error)
		}
	}
	// 許可外のペインには何も届いていない。
	if got := h.TM.Capture(secondIDs[0]); got != before || strings.Contains(got, "FORBIDDEN-MARK") {
		t.Errorf("許可外のペインの画面が変わりました:\n%s", got)
	}
	// 許可が複数（ReportB は書かない＝全部）のトークンは session を省略できない。
	if code := h.ReportB().Call("panes_status", nil).Code(t); code != "session_required" {
		t.Errorf("session 省略: %s", code)
	}
	if ids, _, _ := statusOf(t, h.ReportB(), map[string]any{"session": second}); !slices.Equal(sortedCopy(ids), sortedCopy(secondIDs)) {
		t.Errorf("session=second のペイン = %v", ids)
	}
	if code := h.ReportB().Call("panes_status", map[string]any{"session": "nope"}).Code(t); code != "invalid_args" {
		t.Errorf("未知のセッション: %s", code)
	}
	// 負荷は誰にでも見える（host_status）。agents・tmux_session だけがセッションで絞られる。
	hs := h.Ops().Map("host_status", nil)
	if hs["load"] == nil || hs["tmux_session"] != true {
		t.Errorf("host_status = %v", hs)
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// 受け入れ 6: sessions_list は許可分の layout・panes。all=true でも許可外はスタブ。operable は ops トークン × /ops のときだけ。
func TestSessionsListIntegration(t *testing.T) {
	h, _, _ := twoSessionHarness(t)
	list := func(c *harness.Client, args map[string]any) []map[string]any {
		var out []map[string]any
		for _, s := range c.Map("sessions_list", args)["sessions"].([]any) {
			out = append(out, s.(map[string]any))
		}
		return out
	}
	got := list(h.Ops(), nil)
	if len(got) != 1 || got[0]["id"] != harness.Session || got[0]["operable"] != true ||
		!reflect.DeepEqual(got[0]["panes"], []any{"T1", "T2", "T3", "B1", "B2", "B3"}) {
		t.Fatalf("OpsA の既定 = %v", got)
	}
	all := list(h.Ops(), map[string]any{"all": true})
	if len(all) != 2 || !reflect.DeepEqual(all[1], map[string]any{"id": second, "allowed": false, "operable": false}) {
		t.Errorf("all=true = %v", all)
	}
	if rep := list(h.Report(), nil); len(rep) != 1 || rep[0]["operable"] != false || rep[0]["allowed"] != true {
		t.Errorf("report トークン・/report = %v", rep)
	}
	if viaReport := list(h.Connect("/report", harness.OpsA), nil); len(viaReport) != 1 || viaReport[0]["operable"] != false {
		t.Errorf("ops トークンの /report = %v", viaReport)
	}
	if both := list(h.ReportB(), nil); len(both) != 2 {
		t.Errorf("許可を書かないトークン = %v", both)
	}
}

// resources/list も許可されたペインだけ。
func TestResourcesListIntegration(t *testing.T) {
	h, _, _ := twoSessionHarness(t)
	count := func(c *harness.Client) (uris []string) {
		res, err := c.CS.ListResources(context.Background(), &mcp.ListResourcesParams{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Resources {
			uris = append(uris, r.URI)
		}
		return uris
	}
	for _, c := range []*harness.Client{h.Ops(), h.OpsB()} {
		uris := count(c)
		if len(uris) != 1+2*6 || !slices.Contains(uris, "swarm://report") || !slices.Contains(uris, "swarm://pane/B3") {
			t.Errorf("%s: %v", c.Name, uris)
		}
	}
	if uris := count(h.ReportB()); slices.Contains(uris, "swarm://report") || !slices.Contains(uris, "swarm://pane/second%3AT1") {
		t.Errorf("許可が複数: %v", uris)
	}
}

// 起動時: tokens.toml が config の sessions に無いセッションを許可していれば起動を拒否する。
func TestStartupRefusesUnknownTokenSession(t *testing.T) {
	h := harness.Prepare(t, harness.Options{NoTmuxSession: true})
	if _, err := auth.Add(h.TokensPath(), "stray", auth.ScopeOps, time.Now(), "nope"); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(h.Config, "test", slog.New(slog.DiscardHandler))
	if err == nil || a != nil || !strings.Contains(err.Error(), "stray") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
}
