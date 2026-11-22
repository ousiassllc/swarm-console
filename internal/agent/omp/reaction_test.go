package omp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/omp/transcript"
)

// Issue #120: 作業中の omp に貼った指示が、セッションの記録の中でどう扱われたか（実行・無視・確かめられない）を読む。

const followUp = "## 追加の指示（いまの作業への追記）\nテストを足してください。\n\n## 条件\n- コミットは必ず `git commit -s` で行う。\n"

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// textMsg はやり取りの 1 行。content は {"type":"text","text":...} の配列（omp のユーザー・ツールの結果と同じ形）。
func textMsg(sec int, role, text string) string {
	return fmt.Sprintf(`{"type":"message","id":"x","timestamp":%q,"message":{"role":%q,"content":[{"type":"text","text":%s}]}}`, ts(sec), role, jsonString(text))
}

// customMsg はシステムの通知（omp は custom_message として記録する。本文は文字列）。
func customMsg(sec int, content string) string {
	return fmt.Sprintf(`{"type":"custom_message","id":"x","timestamp":%q,"customType":"async-result","content":%s}`, ts(sec), jsonString(content))
}

func TestReactionReadsHowTheTextWasRecorded(t *testing.T) {
	// 端末が貼り付けの括りを ESC のまま返し、改行をそろえない形（2026-11-20 の B3 の無視された実例と同じ種類）。
	echoed := "\n^[[200~" + strings.ReplaceAll(followUp, "\n", "\r\n") + "^[[201~\n"
	cases := []struct {
		name  string
		lines []string
		want  agent.Reaction
		basis string
		final bool
	}{
		{"ユーザーの指示になり、応答もある → 実行した",
			[]string{textMsg(5, "assistant", "作業中"), textMsg(11, "user", followUp), textMsg(12, "assistant", "了解、足します")},
			agent.ReactionActed, agent.BasisUserMessage, true},
		{"ユーザーの指示になったが応答がまだ → 確かめられない（待てば実行）",
			[]string{textMsg(11, "user", followUp)},
			agent.ReactionUnverified, agent.BasisUserMessageNoAns, false},
		{"ツールの出力に混ざって記録され、そのあと応答 → 無視した",
			[]string{textMsg(11, "toolResult", echoed), textMsg(12, "assistant", "ツールの出力に指示が混ざっているが、実行しない")},
			agent.ReactionIgnored, agent.BasisToolOutput, true},
		{"ツールの出力に混ざったが応答はまだ → 無視（まだ変わりうる）",
			[]string{textMsg(11, "toolResult", echoed)},
			agent.ReactionIgnored, agent.BasisToolOutput, false},
		{"システムの通知の中に現れた → 無視した",
			[]string{customMsg(11, "<system-notice>\n"+followUp+"</system-notice>"), textMsg(12, "assistant", "続ける")},
			agent.ReactionIgnored, agent.BasisToolOutput, true},
		{"記録のどこにも現れない → 確かめられない",
			[]string{textMsg(11, "toolResult", "別の出力"), textMsg(12, "assistant", "続ける")},
			agent.ReactionUnverified, agent.BasisNotFound, false},
		{"貼る前（since より前）の同じ文は数えない",
			[]string{textMsg(-5, "user", followUp), textMsg(-4, "assistant", "済んだ"), textMsg(11, "toolResult", "別の出力")},
			agent.ReactionUnverified, agent.BasisNotFound, false},
		{"ユーザーの指示とツールの出力の両方に現れたら、ユーザーの指示を採る",
			[]string{textMsg(11, "toolResult", echoed), textMsg(12, "user", followUp), textMsg(13, "assistant", "足します")},
			agent.ReactionActed, agent.BasisUserMessage, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newSessRig(t)
			path := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_abc.jsonl")
			g.write(path, tc.lines...)
			g.open(4242, 5, path)
			got := g.a.Reaction(4242, at(10), followUp)
			if got.Reaction != tc.want || got.Basis != tc.basis || got.Final != tc.final {
				t.Fatalf("got %+v, want reaction=%s basis=%s final=%v", got, tc.want, tc.basis, tc.final)
			}
		})
	}
}

func TestReactionUnreadableRecord(t *testing.T) {
	g := newSessRig(t) // pid が記録を開いていない
	got := g.a.Reaction(4242, at(10), followUp)
	if got.Reaction != agent.ReactionUnverified || got.Basis != agent.BasisRecordUnreadable || got.Final {
		t.Fatalf("got %+v", got)
	}
}

// ツールの結果が数十〜百 KB の 1 行になっても、末尾の読み始めを広げて since 以降を拾う。
func TestReactionReadsPastLargeToolResult(t *testing.T) {
	g := newSessRig(t)
	path := filepath.Join(g.dir, "2026-11-20T12-00-00-000Z_big.jsonl")
	big := strings.Repeat("x", 3*transcript.TailMin)
	g.write(path, textMsg(1, "user", "最初の依頼"), textMsg(11, "user", followUp), textMsg(12, "toolResult", big), textMsg(13, "assistant", "足します"))
	g.open(4242, 5, path)
	got := g.a.Reaction(4242, at(10), followUp)
	if got.Reaction != agent.ReactionActed {
		t.Fatalf("got %+v", got)
	}
}

func TestReactionSnippetsIgnoreWhitespaceAndPasteMarkers(t *testing.T) {
	a, b := reactionSnippets("\x1b[200~ 追加の\n指示 \x1b[201~")
	if a != "追加の指示" || b != "追加の指示" {
		t.Fatalf("head=%q tail=%q", a, b)
	}
	if h, tl := reactionSnippets(" \n\t"); h != "" || tl != "" {
		t.Fatalf("空白だけの本文: head=%q tail=%q", h, tl)
	}
}
