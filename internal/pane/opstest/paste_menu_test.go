package opstest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/agenttest"
	_ "github.com/ousiassllc/swarm-console/internal/agent/omp"
	"github.com/ousiassllc/swarm-console/internal/pane"
	"github.com/ousiassllc/swarm-console/internal/pane/panetest"
)

// largePasteLines は omp が貼り付けの確認メニューを開く行数（設定 paste.largeMenuThreshold の既定。v18.20.3 の実機で確認）。
const largePasteLines = 100

// ompLargePaste は実機の omp の大きな貼り付けを再現する擬似端末（Issue #99）。
//   - largePasteLines 行以上を貼ると、貼り付けの確認メニュー（testdata/screens/omp/paste-prompt.txt）が開く。
//     メニューが開いている間の Enter は「Attach as a wrapped block」を選ぶだけで送信にならず、Esc は「Paste inline」。
//     どちらでもメニューが閉じて、本文は入力欄の印（paste-pending.txt）になる。
//   - 印のある入力欄に Enter を送ると送信され、working になる。
//   - それより短い貼り付けは、入力欄に印として載る。
//
// closeIgnored が true のときは、メニューが何のキーでも閉じない（メニューが消えない場合の確認用）。
type ompLargePaste struct {
	t            *testing.T
	f            *fixture
	closeIgnored bool
	// closeAfter は Esc からメニューが消えるまでの偽の時計の経過（0 なら即）。
	closeAfter time.Duration
	// dropEnters は、入力欄に印があっても最初の何回かの Enter を捨てる数（起動直後・高負荷の omp が Enter を取りこぼす。Issue #101）。
	dropEnters int

	mu    sync.Mutex
	menu  bool
	typed bool // 入力欄に貼り付けの印がある
}

func (f *fixture) ompLargePaste(t *testing.T, closeIgnored bool) (pane.Ref, *ompLargePaste) {
	t.Helper()
	f.env.Adapters["omp"] = agenttest.New(t, "omp")
	p := &ompLargePaste{t: t, f: f, closeIgnored: closeIgnored}
	ref := f.single(p.screenText("idle.txt"), []string{"omp"})
	f.env.Tmux.OnOp(p.onOp)
	return ref, p
}

func (p *ompLargePaste) screenText(file string) string {
	return strings.Join(agent.SampleLines(agenttest.Text(p.t, "omp", file)), "\n")
}

func (p *ompLargePaste) show(file string) { p.f.env.Tmux.SetScreen("%0", p.screenText(file)) }

func (p *ompLargePaste) onOp(o panetest.Op) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch o.Kind {
	case "paste":
		if strings.Count(o.Text, "\n")+1 >= largePasteLines {
			p.menu = true
			p.show("paste-prompt.txt")
			return
		}
		p.typed = true
		p.show("paste-pending.txt")
	case "keys":
		for _, k := range o.Keys {
			switch {
			case p.menu && (k == "Enter" || k == "Escape"):
				if !p.closeIgnored {
					p.closeMenu()
				}
			case !p.menu && k == "Enter" && p.typed && p.dropEnters > 0:
				p.dropEnters--
			case !p.menu && k == "Enter" && p.typed:
				p.typed = false
				p.show("working.txt")
			}
		}
	}
}

// closeMenu はメニューを閉じて、本文を入力欄の印にする。closeAfter があれば、その時間が過ぎたあとの最初の画面取得で。
func (p *ompLargePaste) closeMenu() {
	if p.closeAfter <= 0 {
		p.menu, p.typed = false, true
		p.show("paste-pending.txt")
		return
	}
	closeAt := p.f.env.Clock.Now().Add(p.closeAfter)
	p.f.env.Tmux.OnCapture(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.menu && !p.f.env.Clock.Now().Before(closeAt) {
			p.menu, p.typed = false, true
			p.show("paste-pending.txt")
		}
	})
}

// 100 行以上の依頼文: 確認メニューが開いたまま Enter を送ると送信されない。メニューを閉じてから Enter を送って受理される。
func TestSendTextLargePasteMenuIsClosedBeforeEnter(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, false)
	text := strings.TrimSuffix(strings.Repeat("これは長い依頼文の 1 行です。\n", largePasteLines+50), "\n")
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
	got := opStrings(f)
	if len(got) != 3 || !strings.HasPrefix(got[0], "paste %0") || got[1] != "keys %0 Escape" || got[2] != "keys %0 Enter" {
		t.Fatalf("ops = %v（貼り付け → メニューを Esc で閉じる → Enter の順）", got)
	}
}

// dispatch も同じ: 起動済みの idle の omp へ長い依頼文を送って受理される。
func TestDispatchLargePasteMenuIsClosedBeforeEnter(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, false)
	text := strings.TrimSuffix(strings.Repeat("これは長い依頼文の 1 行です。\n", largePasteLines+50), "\n")
	res, err := run(t, f.env.Clock, func() (pane.DispatchResult, error) {
		return f.op.Dispatch(context.Background(), ref, pane.DispatchArgs{Agent: "omp", Text: text})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Accepted || !res.Sent || res.State != agent.StateWorking {
		t.Fatalf("res = %+v", res)
	}
}

// メニューが閉じないときは、Enter を送らず（メニューの選択に化けさせない）ok:false を返す。黙って成功にしない。
func TestSendTextLargePasteMenuStaysOpen(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, true)
	text := strings.TrimSuffix(strings.Repeat("これは長い依頼文の 1 行です。\n", largePasteLines+50), "\n")
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted {
		t.Fatalf("メニューが閉じないのに成功にした: %+v", res)
	}
	if n := f.env.Tmux.CountKeys("Enter"); n != 0 {
		t.Fatalf("メニューが開いたまま Enter を %d 回送った", n)
	}
	if got := f.env.Clock.Now().Sub(panetest.T0); got < f.env.Cfg.AcceptTimeout {
		t.Fatalf("accept_timeout より前に諦めた: %v", got)
	} else if got > time.Second+f.env.Cfg.AcceptTimeout+time.Second {
		t.Fatalf("決着が 1 秒 + accept_timeout を超えた: %v（NFR-PERF-4）", got)
	}
}

// メニューを閉じる待ちと受理待ちは 1 つの期限を分け合う: accept_timeout の終わり際にメニューが閉じ、
// Enter で受理されなくても、決着は 1 秒 + accept_timeout（+ 偽の時計の刻み）以内。
func TestSendTextLargePasteMenuClosingLateKeepsDeadline(t *testing.T) {
	f := newFixture(t)
	ref, p := f.ompLargePaste(t, false)
	p.closeAfter = f.env.Cfg.AcceptTimeout - time.Second
	f.env.Tmux.OnOp(func(o panetest.Op) {
		p.onOp(o)
		if o.Kind == "keys" && len(o.Keys) > 0 && o.Keys[0] == "Enter" {
			p.show("paste-pending.txt") // Enter を送っても受理されない（working にならない）
		}
	})
	text := strings.TrimSuffix(strings.Repeat("これは長い依頼文の 1 行です。\n", largePasteLines+50), "\n")
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted {
		t.Fatalf("受理されないのに成功にした: %+v", res)
	}
	if got := f.env.Clock.Now().Sub(panetest.T0); got > time.Second+f.env.Cfg.AcceptTimeout+time.Second {
		t.Fatalf("決着が 1 秒 + accept_timeout を超えた: %v（NFR-PERF-4）", got)
	}
}

// 画面が読めないときは、メニューの有無が分からないので Enter を送らず ok:false を返す（メニューの選択に化けさせない）。
func TestSendTextPasteMenuUnreadableScreenSendsNoEnter(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, false)
	f.env.Tmux.OnOp(func(o panetest.Op) {
		if o.Kind == "paste" {
			f.env.Tmux.SetCaptureErr("%0", errors.New("capture failed"))
		}
	})
	text := strings.TrimSuffix(strings.Repeat("これは長い依頼文の 1 行です。\n", largePasteLines+50), "\n")
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Accepted {
		t.Fatalf("画面が読めないのに成功にした: %+v", res)
	}
	if n := f.env.Tmux.CountKeys("Enter"); n != 0 {
		t.Fatalf("メニューの有無が分からないのに Enter を %d 回送った", n)
	}
}

// 短い依頼文はメニューが出ないので、Esc は送らない（作業中のエージェントを中断してしまうため）。
func TestSendTextShortPasteSendsNoEscape(t *testing.T) {
	f := newFixture(t)
	ref, _ := f.ompLargePaste(t, false)
	text := strings.TrimSuffix(strings.Repeat("短い依頼文\n", 20), "\n")
	res, err := run(t, f.env.Clock, func() (pane.SendResult, error) { return f.op.SendText(context.Background(), ref, text) })
	if err != nil || !res.OK || !res.Accepted {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if n := f.env.Tmux.CountKeys("Escape"); n != 0 {
		t.Fatalf("メニューがないのに Esc を %d 回送った", n)
	}
}
