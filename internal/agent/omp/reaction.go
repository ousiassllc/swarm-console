package omp

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/agent/omp/transcript"
)

var _ agent.ReactionProbe = (*adapter)(nil)

// reactionSnippetRunes は、送った本文の先頭と末尾から取って記録の中で探す長さ（空白を除いた文字数）。
// 全文を探さないのは、ツールの出力に混ざったときに端末が一部を言い換える（タブなど）ことがあるため。先頭と末尾の両方が見つかれば同じ本文とみなす。
const reactionSnippetRunes = 80

var recordPrefixes = [][]byte{[]byte(`{"type":"message"`), []byte(`{"type":"custom_message"`)}

// reactionRecord は omp のセッションの記録（1 行 1 JSON）のうち、本文を持つ行（やり取り "message" と、システムの通知 "custom_message"）の読む項目。
type reactionRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Content   json.RawMessage `json:"content"` // custom_message の本文
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// pasteMarkers は貼り付けの括り（bracketed paste）。端末がツールの出力へそのまま返すと、ESC が `^[` の字として残る。
var pasteMarkers = strings.NewReplacer("\x1b[200~", "", "\x1b[201~", "", "^[[200~", "", "^[[201~", "")

// Reaction は pid の omp が開いているセッションの記録から、text がどう扱われたかを読む（Issue #120）。
//
//   - ユーザーの指示（role=user）として記録され、そのあとにモデルの応答（assistant）がある → acted
//   - ユーザーの指示として記録されたが、応答はまだ → unverified（待てば acted になる）
//   - ユーザーの指示にはならず、ツールの結果（toolResult）やシステムの通知（custom_message）の中に text が現れた → ignored。
//     作業中のツール（長いコマンド）が動いている間に貼ると、貼った文は端末の入力としてそのツールに渡り、その出力に混ざって戻る。
//     omp はそれを外から紛れ込んだ文として扱う（実例: 2026-11-20 の B3。decisions.md P-109）
//   - どこにも現れない・記録が読めない → unverified
//
// 画面は見ない（tern の画面でも普通の画面でも同じ）。since 以降の行だけを見る。
func (a *adapter) Reaction(pid int, since time.Time, text string) agent.ReactionReading {
	path := a.sessionFile(pid)
	if path == "" {
		return agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisRecordUnreadable}
	}
	recs, ok := recordsSince(path, since)
	if !ok {
		return agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisRecordUnreadable}
	}
	return judgeReaction(recs, text)
}

// seenRecord は記録の 1 行を、判定に要る形に直したもの。順序は記録（ファイル）の順。
type seenRecord struct {
	role string // user・assistant・toolResult ほか。custom_message は "custom_message"
	text string // 本文を正規化したもの（assistant は使わないので空）
}

// judgeReaction は since 以降の記録の並び recs から text の扱いを決める。
func judgeReaction(recs []seenRecord, text string) agent.ReactionReading {
	head, tail := reactionSnippets(text)
	if head == "" {
		return agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisNotFound}
	}
	user, foreign := -1, -1
	for i, r := range recs {
		if r.role == "assistant" || !strings.Contains(r.text, head) || !strings.Contains(r.text, tail) {
			continue
		}
		switch r.role {
		case "user":
			if user < 0 {
				user = i
			}
		case "toolResult", "custom_message":
			if foreign < 0 {
				foreign = i
			}
		}
	}
	answeredAfter := func(i int) bool {
		for _, r := range recs[i+1:] {
			if r.role == "assistant" {
				return true
			}
		}
		return false
	}
	switch {
	case user >= 0 && answeredAfter(user):
		return agent.ReactionReading{Reaction: agent.ReactionActed, Basis: agent.BasisUserMessage, Final: true}
	case user >= 0:
		return agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisUserMessageNoAns}
	case foreign >= 0:
		return agent.ReactionReading{Reaction: agent.ReactionIgnored, Basis: agent.BasisToolOutput, Final: answeredAfter(foreign)}
	}
	return agent.ReactionReading{Reaction: agent.ReactionUnverified, Basis: agent.BasisNotFound}
}

// reactionSnippets は text を正規化し、先頭と末尾の reactionSnippetRunes 文字を返す。
func reactionSnippets(text string) (head, tail string) {
	rs := []rune(normalizeForMatch(text))
	if len(rs) == 0 {
		return "", ""
	}
	n := min(len(rs), reactionSnippetRunes)
	return string(rs[:n]), string(rs[len(rs)-n:])
}

// normalizeForMatch は貼り付けの括りと空白をすべて除く（端末が改行・空白を変えても同じ本文と照合できるように）。
func normalizeForMatch(s string) string {
	s = pasteMarkers.Replace(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// recordsSince は path の末尾から、since 以降の本文を持つ行を記録の順に返す。
// 末尾 sessionTailMin から読み始め、since より前の行に届かなければ倍にして transcript.TailMax まで広げる（ツールの結果の 1 行が数十〜百 KB になるため）。
func recordsSince(path string, since time.Time) ([]seenRecord, bool) {
	f, err := os.Open(path) //nolint:gosec // 名前は /proc/<pid>/fd が指す、~/.omp/agent/sessions の下のセッションの記録だけ
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	var recs []seenRecord
	err = transcript.GrowTail(f, sessionTailMin, transcript.TailMax, func(buf []byte, atStart bool) bool {
		var reachedOlder bool
		recs, reachedOlder = recordsInTail(buf, atStart, since)
		return reachedOlder
	})
	if err != nil {
		return nil, false
	}
	return recs, true
}

// recordsInTail は buf（末尾の窓の中の完全な行。atStart は窓がファイルの先頭に届いたこと）から since 以降の行を返す。
// reachedOlder は since より前の行まで読めたか（これ以上遡らなくてよいか）。
func recordsInTail(buf []byte, atStart bool, since time.Time) (recs []seenRecord, reachedOlder bool) {
	reachedOlder = atStart // ファイルの先頭まで読んだ
	for len(buf) > 0 {
		line := buf
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			line, buf = buf[:i], buf[i+1:]
		} else {
			buf = nil
		}
		if !hasRecordPrefix(line) {
			continue
		}
		var r reactionRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, r.Timestamp)
		if err != nil {
			continue
		}
		if at.Before(since) {
			reachedOlder = true
			continue
		}
		recs = append(recs, toSeenRecord(r))
	}
	return recs, reachedOlder
}

func hasRecordPrefix(line []byte) bool {
	for _, p := range recordPrefixes {
		if bytes.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func toSeenRecord(r reactionRecord) seenRecord {
	if r.Type == "custom_message" {
		return seenRecord{role: "custom_message", text: normalizeForMatch(flattenStrings(r.Content))}
	}
	if r.Message.Role == "assistant" {
		return seenRecord{role: "assistant"}
	}
	return seenRecord{role: r.Message.Role, text: normalizeForMatch(flattenStrings(r.Message.Content))}
}

// flattenStrings は JSON の本文（文字列か、{"type":"text","text":"..."} などの配列）の文字列の値をつなげる。
func flattenStrings(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			b.WriteString(x)
			b.WriteByte('\n')
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return b.String()
}
