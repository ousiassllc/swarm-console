package startoptstest

import (
	"context"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// shellStarting は shell のペインを作り、name の起動コマンドで始まる行の Enter でエージェントが起動するようにする。
func (f *fixture) shellStarting(name, screen string) pane.Ref {
	ref := f.single(panetest.ScreenShell(), nil)
	var last string
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "keys" && o.Keys[0] == "Enter" && len(last) >= len(name) && last[:len(name)] == name:
			f.becomeAgent([]string{name}, screen)
		}
	})
	return ref
}

func opts(model, thinking string) agent.StartOptions {
	return agent.StartOptions{Model: model, Thinking: thinking}
}

// pane_start：指定した旗だけが起動行の末尾に付き、実際に送った行が結果に載る。
func TestStartAppendsOptionFlags(t *testing.T) {
	for _, tc := range []struct {
		name, agent string
		opts        agent.StartOptions
		line        string
	}{
		{"omp 両方", "omp", opts("anthropic/claude-opus-5-5", "high"), "omp --model=anthropic/claude-opus-5-5 --thinking=high"},
		{"omp model だけ", "omp", opts("@slow", ""), "omp --model=@slow"},
		{"omp thinking だけ", "omp", opts("", "xhigh"), "omp --thinking=xhigh"},
		{"claude 両方", "claude", opts("claude-opus-5-5", "max"), "claude --model=claude-opus-5-5 --effort=max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ref := f.shellStarting(tc.agent, panetest.ScreenIdle(tc.agent))
			res, err := run(t, f.env.Clock, func() (pane.StartResult, error) {
				return f.op.Start(context.Background(), ref, tc.agent, "/work", "t", tc.opts)
			})
			if err != nil || !res.OK || res.StartCommand != tc.line {
				t.Fatalf("res = %+v, err = %v", res, err)
			}
			wantOps := []string{`title %0 "agent: t"`, `literal %0 "cd '/work'"`, "keys %0 Enter", `literal %0 "` + tc.line + `"`, "keys %0 Enter"}
			if got := opStrings(f); !equalStrings(got, wantOps) {
				t.Fatalf("ops = %v", got)
			}
		})
	}
}

// 指定が無ければ、これまでと同じ行を送り、結果の StartCommand は空（応答にキーを出さない）。
func TestStartWithoutOptionsKeepsTheLine(t *testing.T) {
	f := newFixture(t)
	ref := f.shellStarting("omp", panetest.ScreenIdle("omp"))
	res, err := run(t, f.env.Clock, func() (pane.StartResult, error) {
		return f.op.Start(context.Background(), ref, "omp", "/work", "t", agent.StartOptions{})
	})
	if err != nil || !res.OK || res.StartCommand != "" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := opStrings(f); got[3] != `literal %0 "omp"` {
		t.Fatalf("ops = %v", got)
	}
}

// 値の不正・エージェントに合わない指定は、何も送らずに拒否する（タイトルも cd も送らない）。
func TestStartRejectsBadOptionsWithoutSending(t *testing.T) {
	for _, tc := range []struct {
		name, agent string
		opts        agent.StartOptions
		want        apperr.Code
	}{
		{"model の注入", "omp", opts("x; touch /tmp/pwned", ""), apperr.InvalidModel},
		{"thinking の注入", "omp", opts("", "high; id"), apperr.InvalidThinking},
		{"claude で off", "claude", opts("", "off"), apperr.InvalidThinking},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ref := f.single(panetest.ScreenShell(), nil)
			_, err := f.op.Start(context.Background(), ref, tc.agent, "/work", "t", tc.opts)
			if code(err) != tc.want {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
			if got := f.env.Tmux.Ops(); len(got) != 0 {
				t.Fatalf("何かを送った: %v", got)
			}
		})
	}
}

// dispatch：shell なら旗付きで起動してから指示を送る。結果に実際の起動行が載る。
func TestDispatchShellStartsWithOptions(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	var last string
	pasted := false
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "literal":
			last = o.Text
		case o.Kind == "paste":
			pasted = true
		case o.Kind == "keys" && o.Keys[0] == "Enter" && pasted:
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "指示を実行"))
		case o.Kind == "keys" && o.Keys[0] == "Enter" && len(last) >= 3 && last[:3] == "omp":
			f.becomeAgent([]string{"omp"}, panetest.ScreenIdle("omp"))
		}
	})
	a := dispatchArgs()
	a.Start = opts("anthropic/claude-opus-5-5", "high")
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) { return f.op.Dispatch(context.Background(), ref, a) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Started || !res.Sent || res.StartCommand != "omp --model=anthropic/claude-opus-5-5 --thinking=high" {
		t.Fatalf("res = %+v", res)
	}
	if got := opStrings(f); got[3] != `literal %0 "omp --model=anthropic/claude-opus-5-5 --thinking=high"` {
		t.Fatalf("ops = %v", got)
	}
}

// 起動済み（idle）のペインに model・thinking を付けると、キーもペーストも送らない。
func TestDispatchIdleRejectsStartOptions(t *testing.T) {
	for _, o := range []agent.StartOptions{opts("anthropic/claude-opus-5-5", ""), opts("", "high"), opts("m", "low")} {
		f := newFixture(t)
		ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
		f.env.FS.Put("/reports/T1.md", "古い報告")
		a := pane.DispatchArgs{Agent: "omp", Text: instruction, Start: o}
		_, err := f.op.Dispatch(context.Background(), ref, a)
		if code(err) != apperr.StartOptionsNA {
			t.Fatalf("%+v: err = %v", o, err)
		}
		if got := f.env.Tmux.Ops(); len(got) != 0 {
			t.Fatalf("%+v: 何かを送った: %v", o, got)
		}
		if !f.env.FS.Has("/reports/T1.md") {
			t.Fatalf("%+v: 古い最終報告を移した", o)
		}
	}
}

// working・asking は model・thinking の有無によらず invalid_state のまま（指定で緩めない）。
func TestDispatchBusyPaneStaysInvalidState(t *testing.T) {
	for name, screen := range map[string]string{
		"working": panetest.ScreenWorking("omp", "1", "x"),
		"asking":  panetest.ScreenAsk("omp", "q", 1, false, "A"),
	} {
		f := newFixture(t)
		ref := f.single(screen, []string{"omp"})
		a := pane.DispatchArgs{Agent: "omp", Text: "x", Start: opts("m", "high")}
		if _, err := f.op.Dispatch(context.Background(), ref, a); code(err) != apperr.InvalidState {
			t.Errorf("%s: err = %v", name, err)
		}
		if got := f.env.Tmux.Ops(); len(got) != 0 {
			t.Errorf("%s: 何かを送った: %v", name, got)
		}
	}
}

// shell への dispatch で指定が不正なら、起動の前に拒否する（何も送らない）。
func TestDispatchShellRejectsBadOptionsWithoutSending(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenShell(), nil)
	a := dispatchArgs()
	a.Start = opts("a b", "")
	if _, err := f.op.Dispatch(context.Background(), ref, a); code(err) != apperr.InvalidModel {
		t.Fatalf("err = %v", err)
	}
	if got := f.env.Tmux.Ops(); len(got) != 0 {
		t.Fatalf("何かを送った: %v", got)
	}
}
