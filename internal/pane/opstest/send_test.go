package opstest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/ops"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

func TestSendTextIdleAccepted(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	var pasteAt, enterAt time.Time
	f.env.Tmux.OnOp(func(o panetest.Op) {
		switch {
		case o.Kind == "paste":
			pasteAt = f.env.Clock.Now()
		case o.Kind == "keys" && o.Keys[0] == "Enter":
			enterAt = f.env.Clock.Now()
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "処理"))
		}
	})
	text := "こんにちは\n2 行目\t字下げ"
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || res.State != agent.StateWorking || res.Chars != len([]rune(text)) {
		t.Fatalf("res = %+v", res)
	}
	if got := opStrings(f); len(got) != 2 || !strings.HasPrefix(got[0], "paste %0") || got[1] != "keys %0 Enter" {
		t.Fatalf("ops = %v", got)
	}
	if enterAt.Sub(pasteAt) < time.Second {
		t.Fatalf("貼り付けから Enter まで %v（1 秒待つ決まり）", enterAt.Sub(pasteAt))
	}
}

func TestSendTextNotAccepted(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"}) // Enter を送っても working にならない
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, "x") })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted || res.State != agent.StateIdle || res.Chars != 1 {
		t.Fatalf("res = %+v", res)
	}
	if f.env.Clock.Now().Sub(panetest.T0) < f.env.Cfg.AcceptTimeout {
		t.Fatalf("accept_timeout より前に諦めた: %v", f.env.Clock.Now().Sub(panetest.T0))
	}
}

func TestSendTextWhileWorkingNeedsInputToClear(t *testing.T) {
	f := newFixture(t)
	ref := f.single(panetest.ScreenWorking("omp", "1", "処理"), []string{"omp"})
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "paste" { // 貼った本文が入力欄に載る
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "1", "処理")+o.Text)
		}
		if o.Kind == "keys" { // Enter で入力欄が空になる
			f.env.Tmux.SetScreen("%0", panetest.ScreenWorking("omp", "2", "処理"))
		}
	})
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, "追加の指示") })
	if err != nil || !res.OK || !res.Accepted || res.State != agent.StateWorking {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestSendTextRejections(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cases := []struct {
		name   string
		screen string
		argv   []string
		text   string
		want   apperr.Code
	}{
		{"asking", panetest.ScreenAsk("omp", "q", 1, false, "A"), []string{"omp"}, "x", apperr.InvalidState},
		{"shell", panetest.ScreenShell(), nil, "x", apperr.InvalidState},
		{"制御文字 ESC", panetest.ScreenIdle("omp"), []string{"omp"}, "a\x1bb", apperr.ForbiddenControlChar},
		{"制御文字 CR", panetest.ScreenIdle("omp"), []string{"omp"}, "a\rb", apperr.ForbiddenControlChar},
		{"長すぎる", panetest.ScreenIdle("omp"), []string{"omp"}, strings.Repeat("あ", 8001), apperr.TextTooLong},
		{"空", panetest.ScreenIdle("omp"), []string{"omp"}, "  ", apperr.InvalidArgs},
	}
	for _, tc := range cases {
		ref := f.single(tc.screen, tc.argv)
		_, err := f.op.SendText(ctx, ref, tc.text)
		if code(err) != tc.want {
			t.Errorf("%s: err = %v, want %s", tc.name, err, tc.want)
		}
	}
	if ops := f.env.Tmux.Ops(); len(ops) != 0 {
		t.Fatalf("拒否のときは何も送らない: %v", ops)
	}
	// invalid_state の details は pane と state。
	ref := f.single(panetest.ScreenShell(), nil)
	_, err := f.op.SendText(ctx, ref, "x")
	ae := apperr.As(err)
	if ae.Details["pane"] != "T1" || ae.Details["state"] != "shell" {
		t.Fatalf("details = %v", ae.Details)
	}
}

func TestSendTextUsesInjectedCheckText(t *testing.T) {
	f := newFixture(t)
	reg, err := pane.NewRegistry(pane.Deps{
		Cfg: f.env.Cfg, Tmux: f.env.Tmux, Host: f.env.Host, Adapters: f.env.Adapters, Notes: f.env.Notes,
		Clock: f.env.Clock, FS: f.env.FS,
		CheckText: func(string) error { return apperr.New(apperr.SecretDetected) },
	})
	if err != nil {
		t.Fatal(err)
	}
	op := ops.New(reg)
	ref := f.single(panetest.ScreenIdle("omp"), []string{"omp"})
	if _, err := op.SendText(context.Background(), ref, "x"); code(err) != apperr.SecretDetected {
		t.Fatalf("err = %v", err)
	}
}

func TestEscape(t *testing.T) {
	cases := []struct {
		name        string
		before      string
		after       string // Esc 後の画面。"" は変わらない
		wantOK      bool
		wantInt     bool
		wantState   agent.State
		argv        []string
		wantInvalid bool
	}{
		{"working → idle", panetest.ScreenWorking("omp", "1", "x"), panetest.ScreenIdle("omp"), true, true, agent.StateIdle, []string{"omp"}, false},
		{"asking → idle", panetest.ScreenAsk("omp", "q", 1, false, "A"), panetest.ScreenIdle("omp"), true, true, agent.StateIdle, []string{"omp"}, false},
		{"変わらない", panetest.ScreenWorking("omp", "1", "x"), "", false, false, agent.StateWorking, []string{"omp"}, false},
		{"idle は変わらず ok=false", panetest.ScreenIdle("omp"), "", false, false, agent.StateIdle, []string{"omp"}, false},
		{"shell は拒否", panetest.ScreenShell(), "", false, false, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ref := f.single(tc.before, tc.argv)
			if tc.after != "" {
				f.env.Tmux.OnOp(func(o panetest.Op) {
					if o.Kind == "keys" && o.Keys[0] == "Escape" {
						f.env.Tmux.SetScreen("%0", tc.after)
					}
				})
			}
			res, err := run(t, f.env.Clock, func() (pane.EscapeResult, error) { return f.op.Escape(context.Background(), ref) })
			if tc.wantInvalid {
				if code(err) != apperr.InvalidState || len(f.env.Tmux.Ops()) != 0 || len(f.env.Notes.Logs()) != 0 {
					t.Fatalf("err=%v ops=%v logs=%v", err, f.env.Tmux.Ops(), f.env.Notes.Logs())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.OK != tc.wantOK || res.Interrupted != tc.wantInt || res.State != tc.wantState {
				t.Fatalf("res = %+v", res)
			}
			if got := opStrings(f); len(got) != 1 || got[0] != "keys %0 Escape" {
				t.Fatalf("ops = %v", got)
			}
			// FR-NOTE-10: 送ったら変化の有無によらずログに残す。
			if logs := f.env.Notes.Logs(); len(logs) != 1 || logs[0] != "%0: Esc を送った（中断）" {
				t.Fatalf("logs = %v", logs)
			}
		})
	}
}

func TestEscapeLogFailureDoesNotChangeResult(t *testing.T) {
	f := newFixture(t)
	f.env.Notes.LogErr = apperr.New(apperr.Internal)
	ref := f.single(panetest.ScreenWorking("omp", "1", "x"), []string{"omp"})
	f.env.Tmux.OnOp(func(panetest.Op) { f.env.Tmux.SetScreen("%0", panetest.ScreenIdle("omp")) })
	res, err := run(t, f.env.Clock, func() (pane.EscapeResult, error) { return f.op.Escape(context.Background(), ref) })
	if err != nil || !res.OK || !res.Interrupted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
