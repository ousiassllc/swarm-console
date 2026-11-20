// Package transcript は omp のセッションの記録（~/.omp/agent/sessions の jsonl）の末尾を読み、やり取り（指示・応答・ツールの呼び出し）と、
// ask ツールの質問を取り出す。omp の画面（capture-pane）が使えない tern の画面（TSP）の読み取りの土台（Issue #121）。
package transcript

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// 会話の記録（セッションの jsonl）の末尾から読む量。1 行（ツールの結果）が数十〜百 KB になることがあるので、やり取りが見つからなければ上限まで倍に広げる。
var messagePrefix = []byte(`{"type":"message"`)

const (
	transcriptTailMin = 256 << 10
	transcriptTailMax = 2 << 20
	// transcriptMessages は画面を組み立てるために読むやり取りの上限（新しいほうから）。
	transcriptMessages = 40
)

// Message は会話の記録のやり取り 1 つ（"type":"message" の行）。
type Message struct {
	At    time.Time
	Role  string // user | assistant | toolResult（ほかの役は読まない）
	Stop  string // assistant の stopReason（toolUse・stop・aborted・error など）
	Text  string // text の部分をつないだもの
	Calls []Call
}

// Call はモデルのツールの呼び出し。
type Call struct {
	ID     string
	Name   string
	Intent string
	Args   json.RawMessage
}

// rawMessage は記録の行のうち、読む項目。content は文字列か、ブロックの配列。
type rawMessage struct {
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role       string          `json:"role"`
		StopReason string          `json:"stopReason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Intent    string          `json:"intent"`
	Arguments json.RawMessage `json:"arguments"`
}

// Read は path の末尾から、新しいほうの最大 transcriptMessages 個のやり取りを古い順で返す。
// ファイルが読めないときは ok=false。やり取りが 1 つも無い（起動しただけ）ときは ok=true で空。
func Read(path string) (msgs []Message, ok bool) {
	f, err := os.Open(path) //nolint:gosec // 名前は /proc/<pid>/fd が指す、~/.omp/agent/sessions の下のセッションの記録だけ
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, false
	}
	for n := int64(transcriptTailMin); ; n *= 2 {
		n = min(n, transcriptTailMax)
		buf, err := readTail(f, st.Size(), n)
		if err != nil {
			return nil, false
		}
		msgs = parseMessages(buf)
		if len(msgs) > 0 || n >= transcriptTailMax || n >= st.Size() {
			return msgs, true
		}
	}
}

// readTail は f の末尾 n バイトを返す。途中から読み始めたときは、切れた最初の行を捨てる。
func readTail(f *os.File, size, n int64) ([]byte, error) {
	off := max(size-n, 0)
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil, err
	}
	if off > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return nil, nil
		}
		buf = buf[i+1:]
	}
	return buf, nil
}

// parseMessages は記録の行から、新しいほうの最大 transcriptMessages 個のやり取りを古い順で返す。
func parseMessages(buf []byte) []Message {
	var rev []Message
	for end := len(buf); end > 0 && len(rev) < transcriptMessages; {
		start := bytes.LastIndexByte(buf[:end], '\n') + 1
		line := buf[start:end]
		end = start - 1
		if !bytes.HasPrefix(line, messagePrefix) {
			continue
		}
		if m, ok := parseMessage(line); ok {
			rev = append(rev, m)
		}
	}
	msgs := make([]Message, len(rev))
	for i, m := range rev {
		msgs[len(rev)-1-i] = m
	}
	return msgs
}

// parseMessage は記録の 1 行を message にする。時刻が読めない・役が user・assistant・toolResult でないものは ok=false。
func parseMessage(line []byte) (Message, bool) {
	var r rawMessage
	if json.Unmarshal(line, &r) != nil {
		return Message{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil {
		return Message{}, false
	}
	switch r.Message.Role {
	case "user", "assistant", "toolResult":
	default:
		return Message{}, false
	}
	m := Message{At: at, Role: r.Message.Role, Stop: r.Message.StopReason}
	var text string
	if json.Unmarshal(r.Message.Content, &text) == nil {
		m.Text = text
		return m, true
	}
	var blocks []rawBlock
	if json.Unmarshal(r.Message.Content, &blocks) != nil {
		return m, true
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "toolCall":
			m.Calls = append(m.Calls, Call{ID: b.ID, Name: b.Name, Intent: b.Intent, Args: b.Arguments})
		}
	}
	m.Text = strings.Join(parts, "\n")
	return m, true
}

// InTurn は最後のやり取りで番が終わっていないか。ユーザーの指示・ツールの結果・ツールの呼び出し（assistant で stopReason=toolUse）は番の途中
// （omp アダプタの session.go の lastTurnIn と同じ規則）。やり取りが無ければ false。
func InTurn(msgs []Message) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	switch last.Role {
	case "user", "toolResult":
		return true
	default:
		return last.Stop == "toolUse"
	}
}

// PendingAsk は番の途中で、最後のやり取りが ask ツールの呼び出し（結果がまだ無い）ならその呼び出しを返す。
func PendingAsk(msgs []Message) (Call, bool) {
	if len(msgs) == 0 || !InTurn(msgs) {
		return Call{}, false
	}
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" {
		return Call{}, false
	}
	for i := len(last.Calls) - 1; i >= 0; i-- {
		if last.Calls[i].Name == "ask" {
			return last.Calls[i], true
		}
	}
	return Call{}, false
}

// TurnStart は進行中の番の始まり（最後のユーザーの指示の時刻）。見つからなければ読んだ最初のやり取りの時刻。
func TurnStart(msgs []Message) time.Time {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].At
		}
	}
	if len(msgs) > 0 {
		return msgs[0].At
	}
	return time.Time{}
}

// LastCall は最後のやり取り（ツールの結果を除く）が assistant のツールの呼び出しなら、その呼び出しの最後を返す。
func LastCall(msgs []Message) (Call, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "toolResult" {
			continue
		}
		if msgs[i].Role == "assistant" && len(msgs[i].Calls) > 0 {
			return msgs[i].Calls[len(msgs[i].Calls)-1], true
		}
		return Call{}, false
	}
	return Call{}, false
}
