package omp

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/omp/transcript"
)

var _ agent.Native = (*adapter)(nil)

// nativeTitlePrefix は tern の画面（TSP）の omp が、APC で出して tmux が pane_title に取り込む最後のフレームの接頭辞（`tsp;f;{...}`）。
const nativeTitlePrefix = "tsp;"

// 組み立てる画面の目印。実画面（testdata/screens/omp）の形に合わせてあり、Detect・ParseCard・Clean がそのまま読める。
const (
	nativeActivityGlyph = '\U000f12b7' // 活動行の記号（Nerd Font の私用領域。unicode.Co）
	nativeSpinner       = "⠋"
	nativeLabel         = "tern"
	nativeBoxWidth      = 100
)

// NativeTitle は pane_title が tern の画面データか。
func (a *adapter) NativeTitle(title string) bool {
	return strings.HasPrefix(title, nativeTitlePrefix)
}

// NativeScreen は pid の omp の会話の記録から、普通の画面と同じ形の画面を組み立てる（Issue #121）。
// 上から、直近のやり取り（指示・応答・ツールの呼び出し）、質問が出ていれば質問カード、作業中なら活動行、ステータス行。
// 作業中かどうかは記録の最後のやり取り（session.go と同じ規則）、質問が出ているかは最後のやり取りが ask ツールの呼び出しかで決める。
// 記録のファイルを開いていない（まだ何も話していない）omp は、やり取りの無い idle の画面にする。読めなければ ok=false。
func (a *adapter) NativeScreen(pid int, maxLines int) (agent.NativeView, bool) {
	var msgs []transcript.Message
	if path := a.sessionFile(pid); path != "" {
		var ok bool
		if msgs, ok = transcript.Read(path); !ok {
			return agent.NativeView{}, false
		}
	}
	lines, questions := a.compose(msgs, maxLines)
	return agent.NativeView{Screen: agent.NewScreen(lines), AskQuestions: questions}, true
}

// compose は msgs（古い順）から、画面の最後 maxLines 行までと、質問カードが出ているときのフォームの質問の数を作る。
func (a *adapter) compose(msgs []transcript.Message, maxLines int) (lines []string, questions int) {
	body := renderMessages(msgs)
	var chrome []string
	working := transcript.InTurn(msgs)
	activity := "Working…"
	if call, ok := transcript.PendingAsk(msgs); ok {
		if qs := transcript.ParseAsk(call.Args); len(qs) > 0 {
			chrome = append(chrome, askCard(qs[0])...)
			chrome = append(chrome, "")
			questions = len(qs)
		}
		activity = "Asking"
		if call.Intent != "" {
			activity = call.Intent
		}
	} else if c, ok := transcript.LastCall(msgs); ok && c.Intent != "" {
		activity = c.Intent
	}
	elapsed := ""
	if working {
		chrome = append(chrome, "  "+string(nativeActivityGlyph)+" "+activity)
		elapsed = " " + formatElapsed(a.clock().Sub(transcript.TurnStart(msgs)))
	}
	status := string(statusCap) + " "
	if working {
		status += nativeSpinner + elapsed + " "
	}
	status += string(statusSep) + " " + nativeLabel
	lines = append(body, chrome...)
	lines = append(lines, status)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, questions
}

func (a *adapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// renderMessages はやり取りを、1 行の字下げ 1 でつないだ本文にする。ツールの結果は長いので出さない。
// 指示・応答の本文は 1 行ずつ、ツールの呼び出しは `▸ <名前>: <意図>` の 1 行にして、やり取りの間は空行にする。
func renderMessages(msgs []transcript.Message) []string {
	var out []string
	for _, m := range msgs {
		var block []string
		if text := strings.TrimRight(m.Text, "\n"); text != "" {
			for l := range strings.SplitSeq(text, "\n") {
				block = append(block, " "+strings.TrimRight(l, " \t"))
			}
		}
		for _, c := range m.Calls {
			l := " ▸ " + c.Name
			if c.Intent != "" {
				l += ": " + c.Intent
			}
			block = append(block, l)
		}
		if len(block) == 0 {
			continue
		}
		out = append(out, block...)
		out = append(out, "")
	}
	return out
}

// formatElapsed は経過時間を omp のステータス行の形（`36s`・`17m05s`・`1h05m`）にする。
func formatElapsed(d time.Duration) string {
	s := max(int(d.Seconds()), 0)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	default:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	}
}

// NativeDelivered は since 以降に、text の最後の行の末尾（30 字）を含むユーザーの指示が記録されたか（agent.Native）。
func (a *adapter) NativeDelivered(pid int, text string, since time.Time) bool {
	path := a.sessionFile(pid)
	if path == "" {
		return false
	}
	msgs, ok := transcript.Read(path)
	if !ok {
		return false
	}
	tail := lastLineTail(text)
	if tail == "" {
		return false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.At.Before(since) {
			return false
		}
		if m.Role == "user" && strings.Contains(m.Text, tail) {
			return true
		}
	}
	return false
}

// lastLineTail は text の最後の非空行の末尾 30 字（ops の受理確認と同じ長さ）。
func lastLineTail(text string) string {
	parts := strings.Split(strings.TrimRight(text, " \t\r\n"), "\n")
	tail := strings.TrimSpace(parts[len(parts)-1])
	if n := utf8.RuneCountInString(tail); n > 30 {
		tail = string([]rune(tail)[n-30:])
	}
	return tail
}
