package main

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// mode は擬似エージェントの画面の状態。
type mode int

const (
	modeIdle    mode = iota // 入力待ち
	modeWorking             // 作業中（スピナーのコマが tick ごとに変わる）
	modeAsk                 // 質問カード
	modeCustom              // 「その他」の自由入力中
)

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// machine は画面の状態遷移だけを持つ（入出力は main.go）。キーの列 Feed を受けて状態を変え、
// 記録すべき出来事を record に渡す。
type machine struct {
	agent    string
	scenario string
	rows     rows // 質問カードの行の構成
	mode     mode
	cursor   int    // 質問カードのカーソル（1 始まり）
	typed    []byte // 自由入力中の本文
	line     []byte // 入力待ちで打った文字（/exit・/quit の判定用）
	pasted   bool   // 直近の Enter のあとに貼り付けがあったか
	inPaste  bool
	paste    []byte
	tick     int
	quit     bool
	record   func(event)
}

// rows は質問カードの構成。
type rows struct {
	options   int // 選択肢の数（「その他」を除く）
	customRow int // 「その他」の行（なければ 0）。options+1
	start     int // カーソルの初期位置
}

// event は --log に残す出来事。
type event struct {
	kind string // PASTE・PICK・CUSTOM・SUBMIT・ESCAPE
	n    int    // PICK の番号
	text string // PASTE・CUSTOM の本文
}

func newMachine(agent, scenario string, record func(event)) *machine {
	m := &machine{agent: agent, scenario: scenario, record: record}
	switch scenario {
	case "working":
		m.mode = modeWorking
	case "ask", "ask-custom":
		m.rows = askRows(agent, scenario)
		m.mode = modeAsk
		m.cursor = m.rows.start
	}
	return m
}

// askRows は質問カードの行の構成。サンプル画面（testdata/screens）の選択肢に合わせてある。
func askRows(agent, scenario string) rows {
	switch {
	case agent == "omp":
		return rows{options: 3, customRow: 4, start: 2}
	case agent == "claude" && scenario == "ask-custom":
		return rows{options: 3, customRow: 4, start: 2}
	default: // claude の許可確認・codex の承認カードは自由入力の項がない
		return rows{options: 3, start: 1}
	}
}

// lastRow はカーソルが動ける最後の行。
func (m *machine) lastRow() int {
	if m.rows.customRow > 0 {
		return m.rows.customRow
	}
	return m.rows.options
}

// Tick は一定間隔の時報。画面を描き直す必要があれば true。
func (m *machine) Tick() bool {
	if m.mode != modeWorking {
		return false
	}
	m.tick++
	return true
}

// Feed はキー入力のバイト列を処理する。分割された ESC シーケンス（1 回の read をまたぐもの）は扱わない。
func (m *machine) Feed(data []byte) {
	for i := 0; i < len(data) && !m.quit; {
		if m.inPaste {
			end := bytes.Index(data[i:], pasteEnd)
			if end < 0 {
				m.paste = append(m.paste, data[i:]...)
				return
			}
			m.paste = append(m.paste, data[i:i+end]...)
			m.finishPaste()
			i += end + len(pasteEnd)
			continue
		}
		i += m.key(data[i:])
	}
}

// key は先頭の 1 キーを処理して、読み進めたバイト数を返す。
func (m *machine) key(b []byte) int {
	switch c := b[0]; {
	case c == 0x1b:
		return m.escape(b)
	case c == '\r' || c == '\n':
		m.enter()
	case c == 0x7f || c == 0x08:
		m.backspace()
	case c >= 0x20:
		_, size := utf8.DecodeRune(b)
		m.char(b[:size])
		return size
	}
	return 1
}

// escape は ESC で始まる列を処理する。単独の ESC は中断、`ESC [ A/B`（↑/↓）は移動、ほかは読み捨てる。
func (m *machine) escape(b []byte) int {
	switch {
	case bytes.HasPrefix(b, pasteStart):
		m.inPaste, m.paste = true, nil
		return len(pasteStart)
	case len(b) == 1:
		m.interrupt()
		return 1
	case len(b) >= 3 && (b[1] == '[' || b[1] == 'O') && (b[2] == 'A' || b[2] == 'B'):
		m.move(map[byte]int{'A': -1, 'B': 1}[b[2]])
		return 3
	case b[1] == '[':
		n := 2
		for n < len(b) && (b[n] < 0x40 || b[n] > 0x7e) {
			n++
		}
		return min(n+1, len(b))
	}
	return 2
}

func (m *machine) move(delta int) {
	if m.mode != modeAsk {
		return
	}
	m.cursor = min(max(m.cursor+delta, 1), m.lastRow())
}

func (m *machine) interrupt() {
	switch m.mode {
	case modeWorking, modeAsk, modeCustom:
		m.record(event{kind: "ESCAPE"})
		m.mode, m.typed = modeIdle, nil
	}
}

func (m *machine) backspace() {
	switch m.mode {
	case modeIdle:
		m.line = trimLastRune(m.line)
	case modeCustom:
		m.typed = trimLastRune(m.typed)
	}
}

func trimLastRune(b []byte) []byte {
	_, size := utf8.DecodeLastRune(b)
	return b[:len(b)-size]
}

// char は印字できる 1 文字（UTF-8 の 1 文字分のバイト列）を処理する。
func (m *machine) char(c []byte) {
	switch m.mode {
	case modeIdle:
		m.line = append(m.line, c...)
	case modeCustom:
		m.typed = append(m.typed, c...)
	case modeAsk:
		// 数字キーで直接選べるのは Claude Code と Codex（omp は ↑/↓ と Enter だけ）。
		if n := int(c[0] - '0'); len(c) == 1 && n >= 1 && n <= m.lastRow() && m.agent != "omp" {
			m.cursor = n
			m.enter()
		}
	}
}

func (m *machine) enter() {
	switch m.mode {
	case modeIdle:
		cmd := strings.TrimSpace(string(m.line))
		m.line = nil
		switch {
		case cmd == "/exit" || cmd == "/quit":
			m.quit = true
		case m.pasted:
			m.pasted = false
			m.record(event{kind: "SUBMIT"})
			m.mode = modeWorking
		}
	case modeAsk:
		if m.cursor == m.rows.customRow {
			m.mode, m.typed = modeCustom, nil
			return
		}
		m.record(event{kind: "PICK", n: m.cursor})
		m.mode = modeWorking
	case modeCustom:
		m.record(event{kind: "CUSTOM", text: string(m.typed)})
		m.mode, m.typed = modeWorking, nil
	}
}

// finishPaste は貼り付けの本文（CR は LF にそろえる）を記録する。画面には反映しない。
// 自由入力中だけは入力欄の本文にも足す。
func (m *machine) finishPaste() {
	m.inPaste = false
	body := strings.ReplaceAll(strings.ReplaceAll(string(m.paste), "\r\n", "\n"), "\r", "\n")
	m.paste = nil
	m.record(event{kind: "PASTE", text: body})
	m.pasted = true
	if m.mode == modeCustom {
		m.typed = append(m.typed, body...)
	}
}
