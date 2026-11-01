package ops

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/notes"
	"github.com/ousiassllc/swarm-console/internal/pane"
)

// 閉じる手順の名前（API 仕様 §5.2 の steps）。
const (
	stepCD    = "cd ~"
	stepClear = "clear"
	stepTitle = "title"
)

// promptSuffixes はシェルのプロンプトらしい行の末尾。
var promptSuffixes = []string{"$", "#", "%", ">", "❯", "➜", "»"}

// Close はエージェントを終了してシェルを片付け、画面を確かめる。idle・shell のみ（P-19）。
// 確かめられたときだけメモを履歴へ、最終報告を archive/ へ移す。
func (o *Operator) Close(ctx context.Context, ref pane.Ref) (pane.CloseResult, error) {
	unlock, p, err := o.begin(ctx, ref, true)
	if err != nil {
		return pane.CloseResult{}, err
	}
	defer unlock()
	if err := allow(p, agent.StateIdle, agent.StateShell); err != nil {
		return pane.CloseResult{}, err
	}
	type step struct{ name, text string }
	var plan []step
	if p.State == agent.StateIdle {
		ad, err := o.adapterFor(p)
		if err != nil {
			return pane.CloseResult{}, err
		}
		plan = append(plan, step{ad.ExitCommand(), ad.ExitCommand()})
	}
	plan = append(plan, step{stepCD, stepCD}, step{stepClear, stepClear})
	res := pane.CloseResult{Steps: []string{}}
	for _, s := range plan {
		if err := o.typeLine(ctx, p.ID, s.text); err != nil {
			return res, err
		}
		res.Steps = append(res.Steps, s.name)
	}
	if err := o.deps.Tmux.SetTitle(ctx, p.ID, ""); err != nil {
		return res, pane.TmuxError(err)
	}
	res.Steps = append(res.Steps, stepTitle)

	clean, err := o.waitFor(ctx, o.deps.Cfg.AcceptTimeout, pollStep, func(ctx context.Context) bool {
		text, err := o.deps.Tmux.Capture(ctx, p.ID, pane.CaptureLines)
		return err == nil && o.promptOnly(pane.SplitLines(text))
	})
	if err != nil {
		return res, err
	}
	res.State = o.stateOf(ctx, p.ID)
	if !clean {
		return res, nil
	}
	res.OK = true
	o.archiveClosed(ctx, p, &res)
	return res, nil
}

// typeLine は「文字列 → 1 秒 → Enter → 1 秒」を送る。文字列を送った後は、ctx がキャンセルされても
// Enter まで（と最後の 1 秒）送りきる（入力行に半端な文字列を残さない。NFR-AVL-7）。
func (o *Operator) typeLine(ctx context.Context, id, text string) error {
	if err := o.deps.Tmux.SendLiteral(ctx, id, text); err != nil {
		return pane.TmuxError(err)
	}
	ctx = context.WithoutCancel(ctx)
	if err := o.sleep(ctx, fixedWait); err != nil {
		return err
	}
	if err := o.sendKeys(ctx, id, "Enter"); err != nil {
		return err
	}
	return o.sleep(ctx, fixedWait)
}

// archiveClosed は確認できたペインのメモと最終報告を履歴・archive/ へ移す。失敗は結果を変えずログに残す。
func (o *Operator) archiveClosed(ctx context.Context, p pane.PaneState, res *pane.CloseResult) {
	now := o.deps.Clock.Now()
	meta := notes.ArchiveMeta{PaneID: p.ID, Pos: p.Pos, Agent: p.Agent, Title: p.Title, At: now}
	moved, err := o.deps.Notes.Archive(context.WithoutCancel(ctx), meta, notes.ClosedByPaneClose)
	if err != nil {
		o.deps.Logger.Warn("メモを履歴へ移せませんでした", "pane", p.ID, "err", err)
	}
	res.NoteArchived = err == nil && moved
	if _, err := pane.ArchiveReport(o.deps.FS, o.deps.Cfg.ReportDir, p.Pos, now); err != nil {
		o.deps.Logger.Warn("最終報告を archive へ移せませんでした", "pane", p.ID, "err", err)
	}
}

// maxPromptLines は枠付きプロンプトとして認める最大の行数（fish・starship の 2〜3 行）。
const maxPromptLines = 3

// promptOnly は画面が「エージェントが消え、シェルのプロンプトだけ」か。
// 非空行が 1 行以下で、あるならプロンプトらしい行（~ を含むか、$ # % > などで終わる）。
// 複数行は、枠付きプロンプト（先頭が ╭┌ などの上枠、末尾が ╰└ などの下枠で、~ を含む）だけ認める。
func (o *Operator) promptOnly(lines []string) bool {
	var nonBlank []string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			nonBlank = append(nonBlank, t)
		}
	}
	if len(nonBlank) > 1 && !framedPrompt(nonBlank) {
		return false
	}
	scr := agent.NewScreen(lines)
	for _, ad := range o.deps.Adapters {
		if ad != nil && ad.MatchScreen(scr) {
			return false
		}
	}
	if len(nonBlank) == 0 {
		return true
	}
	if len(nonBlank) > 1 {
		return true
	}
	l := nonBlank[0]
	if strings.Contains(l, "~") {
		return true
	}
	for _, s := range promptSuffixes {
		if strings.HasSuffix(l, s) {
			return true
		}
	}
	return false
}

// framedPrompt は非空行（前後の空白は除去済み）が、1 つの枠付きプロンプトか。
// 先頭行が上枠（╭┌╔┏）、末尾行が下枠（╰└╚┗）、間の行が縦線（│├┃║）で始まり、全体のどこかに ~ がある。
// 右端まで閉じた枠（╮┐╗┓ や ╯┘╝┛ で終わる行）は、別のアプリの箱であってプロンプトではない。
func framedPrompt(nonBlank []string) bool {
	n := len(nonBlank)
	if n > maxPromptLines || !startsWithAny(nonBlank[0], "╭┌╔┏") || !startsWithAny(nonBlank[n-1], "╰└╚┗") {
		return false
	}
	if endsWithAny(nonBlank[0], "╮┐╗┓") || endsWithAny(nonBlank[n-1], "╯┘╝┛") {
		return false
	}
	for _, l := range nonBlank[1 : n-1] {
		if !startsWithAny(l, "│├┃║") || utf8.RuneCountInString(l) > 1 && endsWithAny(l, "│┃║") {
			return false
		}
	}
	return strings.Contains(strings.Join(nonBlank, "\n"), "~")
}

// endsWithAny は s が set のどれかの文字で終わるか。
func endsWithAny(s, set string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r != utf8.RuneError && strings.ContainsRune(set, r)
}

// startsWithAny は s が set のどれかの文字で始まるか。
func startsWithAny(s, set string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r != utf8.RuneError && strings.ContainsRune(set, r)
}
