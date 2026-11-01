package pane

import (
	"context"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// pane_tail・ask_card の上限（API 仕様 §7）。
const (
	tailLineMax  = 500
	tailTotalMax = 20000
	cardOptMax   = 20
	cardQMax     = 1000
	cardLabelMax = 300
	argEchoMax   = 100 // pane_not_found の details に載せる引数の最大字数
	cleanMargin  = 30  // Clean で落ちる飾りの分、多めに取る行数
)

// Run はサンプラー。poll_interval ごとに Refresh し、ctx が終わるまで続く。
func (r *SnapshotRegistry) Run(ctx context.Context) error {
	for {
		r.deps.Host.SampleCPU() // CPU のサンプルを更新するのはサンプラーだけ（report-values.md §3）
		if _, err := r.Refresh(ctx); err != nil && ctx.Err() == nil {
			r.log.Warn("スナップショットの更新に失敗しました", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.clk.After(r.deps.Cfg.PollInterval):
		}
	}
}

// Resolve は位置（大文字小文字不問）または tmux ID を解決する。
func (r *SnapshotRegistry) Resolve(ctx context.Context, arg string) (Ref, error) {
	snap, err := r.Current(ctx)
	if err != nil {
		return Ref{}, apperr.Wrap(apperr.Internal, err)
	}
	p, ok := snap.Find(arg)
	if !ok {
		shown, _ := TruncateRunes(arg, argEchoMax)
		return Ref{}, apperr.New(apperr.PaneNotFound, "pane", shown)
	}
	return Ref{ID: p.ID, Pos: p.Pos}, nil
}

// target はスナップショット上の ref のペインを返す。消えていれば pane_not_found。
func (r *SnapshotRegistry) target(ctx context.Context, ref Ref) (*PaneState, error) {
	snap, err := r.Current(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	p, ok := snap.Find(ref.ID)
	if !ok {
		return nil, apperr.New(apperr.PaneNotFound, "pane", ref.Pos)
	}
	return p, nil
}

// capture は画面を取り直して Screen にする。tmux の失敗は tmux_error。
func (r *SnapshotRegistry) capture(ctx context.Context, id string, lines int) ([]string, error) {
	text, err := r.deps.Tmux.Capture(ctx, id, lines)
	if err != nil {
		return nil, TmuxError(err)
	}
	return SplitLines(text), nil
}

// TmuxError は tmux 呼び出しの失敗を apperr.TmuxError に包む（すでに *apperr.Error ならそのまま）。
func TmuxError(err error) error {
	if e := apperr.As(err); e.Code != apperr.Internal {
		return e
	}
	return apperr.Wrap(apperr.TmuxError, err)
}

// Tail は最後の lines 行。raw=false かつエージェントありなら Clean 済み。
// 1 行 500 字・全体 20,000 字（末尾側を残す）で切る。
func (r *SnapshotRegistry) Tail(ctx context.Context, ref Ref, lines int, raw bool) (Tail, error) {
	p, err := r.target(ctx, ref)
	if err != nil {
		return Tail{}, err
	}
	ad, hasAgent := r.Adapter(p.Agent)
	clean := !raw && hasAgent
	n := lines
	if clean {
		n += cleanMargin
	}
	got, err := r.capture(ctx, p.ID, n)
	if err != nil {
		return Tail{}, err
	}
	if clean {
		got = ad.Clean(got)
	}
	if len(got) > lines {
		got = got[len(got)-lines:]
	}
	truncated := false
	out := make([]string, len(got))
	for i, l := range got {
		var t bool
		out[i], t = TruncateRunes(l, tailLineMax)
		truncated = truncated || t
	}
	text := strings.Join(out, "\n")
	if rs := []rune(text); len(rs) > tailTotalMax {
		text, truncated = string(rs[len(rs)-tailTotalMax:]), true
	}
	return Tail{Text: text, Truncated: truncated}, nil
}

// Card は質問カード。asking でなければ (nil, nil)。エージェントなしは unsupported_agent。
func (r *SnapshotRegistry) Card(ctx context.Context, ref Ref) (*agent.Card, error) {
	p, err := r.target(ctx, ref)
	if err != nil {
		return nil, err
	}
	ad, ok := r.Adapter(p.Agent)
	if !ok {
		return nil, apperr.New(apperr.UnsupportedAgent, "pane", p.Pos)
	}
	got, err := r.capture(ctx, p.ID, CaptureLines)
	if err != nil {
		return nil, err
	}
	return ReadCard(ad, got), nil
}

// ReadCard は画面が質問カードなら上限で切ったカードを返す。そうでなければ nil。
func ReadCard(ad agent.Adapter, lines []string) *agent.Card {
	c := RawCard(ad, lines)
	if c == nil {
		return nil
	}
	return limitCard(*c)
}

// RawCard は画面が質問カードなら切らないカードを返す（キー列の計算用）。そうでなければ nil。
func RawCard(ad agent.Adapter, lines []string) *agent.Card {
	screen := agent.NewScreen(lines)
	if ad.Detect(screen).State != agent.StateAsking {
		return nil
	}
	c, ok := ad.ParseCard(screen)
	if !ok {
		return nil
	}
	return &c
}

// limitCard はカードを上限（選択肢 20 個・question 1000 字・label 300 字）で切る。切ったら Truncated。
func limitCard(c agent.Card) *agent.Card {
	var t bool
	c.Question, t = TruncateRunes(c.Question, cardQMax)
	c.Truncated = c.Truncated || t
	opts := c.Options
	if len(opts) > cardOptMax {
		opts, c.Truncated = opts[:cardOptMax], true
	}
	c.Options = make([]agent.Option, len(opts))
	for i, o := range opts {
		o.Label, t = TruncateRunes(o.Label, cardLabelMax)
		c.Truncated = c.Truncated || t
		c.Options[i] = o
	}
	return &c
}
