package omp

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/apperr"
)

var (
	// 選択肢の行: 行頭の余白、任意のカーソル、記号（○・◉・☐・☑）、文言。説明の続き行は記号がないので当たらない。
	optionRE = regexp.MustCompile(`^\s?(` + string(cursorGlyph) + `)?\s*(\p{Co})\s+(\S.*?)\s*$`)
	tabRE    = regexp.MustCompile(`\sSubmit$`)
	// 自由入力の行。v18 は `Other (type your own)`、v19 は `Other: type your own`（カーソルがあると `Other:  type your own`、
	// 入力したあとは `Other: <入力した文>`）。
	customRE      = regexp.MustCompile(`^Other(?: \(type your own\)|:.*)$`)
	recommendedRE = regexp.MustCompile(`(?i)\s*\(recommended\)\s*$`)
	submitFocused = string(cursorGlyph) + " Submit" // カーソルがある Submit の行の中身
)

// 複数選択の選択肢の記号（Nerd Font の square と check-square）。単一選択は丸（U+F10C・U+F192）。
const (
	uncheckedGlyph = '\uf096'
	checkedGlyph   = '\uf14a'
)

// rawOption は枠の中で読んだ選択肢の行。
type rawOption struct {
	cursor bool
	glyph  rune // 行の記号（optionRE が読んだ 2 つ目の \p{Co}。カーソルのあとの記号）
	label  string
}

// ParseCard は質問カード（`╭─ Ask`）から質問・選択肢・カーソルを読む。
// 「Other (type your own)」「Other: …」は Options に入れず AllowsCustom にする（カーソルがそこなら Cursor=len(Options)+1）。
// 自由入力中（`Custom answer`）の枠・選択肢がたたまれた Submit の画面は選ぶ対象がないので ok=false。
//
// 枠の形は 2 通り。v18 は `├` で見出し・選択肢・フッターに区切られ、選択肢は 2 つ目の区画。
// v19 は区切りがなく、見出しのあとに選択肢が続き、最後に Submit の行が来る。質問が複数のフォームは、開いている質問
// （選択肢の行がある質問）だけを読む。選び済みの質問は 1 行にたたまれ、まだの質問は `○ 見出し` と横並びの選択肢で、どちらも選択肢の行の形ではない。
func (a *adapter) ParseCard(s agent.Screen) (agent.Card, bool) {
	c, _, ok := a.readCard(s)
	return c, ok
}

// readCard は ParseCard の本体。選択肢の行（Other を除く、Options と同じ並び）の記号も返す。
func (a *adapter) readCard(s agent.Screen) (agent.Card, []rune, bool) {
	lines := agent.TrimScreen(s.Lines)
	span, ok := a.liveCard(lines)
	if !ok || strings.Contains(lines[span.top], "Custom answer") {
		return agent.Card{}, nil, false
	}
	region := lines[span.top+1 : span.bottom]
	sectioned := slices.ContainsFunc(region, func(l string) bool { return strings.HasPrefix(l, "├") })
	var (
		head, para []string // para は直近の空行までの見出しの候補
		opts       []rawOption
		section    int
		prevOpt    bool // 直前の行が選択肢（折り返しの続き行を足す先）
		done       bool // 選択肢のあとの空行か区切り。以降は読まない
	)
	for _, l := range region {
		if strings.HasPrefix(l, "├") {
			section++
			done = done || len(opts) > 0
			continue
		}
		inner, boxed := agent.BoxInner(l)
		if !boxed || done {
			continue
		}
		t := strings.TrimSpace(inner)
		m := optionRE.FindStringSubmatch(inner)
		switch {
		case t == "":
			if len(opts) > 0 {
				done = true
			}
			para = nil
		case t == submitFocused:
		case m != nil && (!sectioned || section == 1):
			if len(opts) == 0 {
				head = para
			}
			r, _ := utf8.DecodeRuneInString(m[2])
			opts = append(opts, rawOption{cursor: m[1] != "", glyph: r, label: m[3]})
			prevOpt = true
		case tabRE.MatchString(t):
		case len(opts) > 0:
			if prevOpt { // 長い選択肢の折り返し
				opts[len(opts)-1].label = joinWrapped(opts[len(opts)-1].label, t)
			}
		default:
			para = append(para, t)
		}
	}
	c := agent.Card{Question: joinWrapped(head...)}
	var glyphs []rune
	for _, o := range opts {
		label := recommendedRE.ReplaceAllString(o.label, "")
		if customRE.MatchString(label) {
			c.AllowsCustom = true
			if o.cursor {
				c.Cursor = len(c.Options) + 1
			}
			continue
		}
		c.Options = append(c.Options, agent.Option{N: len(c.Options) + 1, Label: label, Recommended: recommendedRE.MatchString(o.label)})
		glyphs = append(glyphs, o.glyph)
		if o.cursor {
			c.Cursor = len(c.Options)
		}
	}
	return c, glyphs, len(c.Options) > 0
}

// ToggleKeys は複数選択（チェックボックス）の質問で n 番を選ぶために、カーソルを n に合わせたあとに送るキー（agent.Toggler）。
// 画面に n 番がまだチェックされていないチェックボックスで出ているときだけ Space を返す。チェック済み・単一選択（丸）・読めない画面では空。
// v19 の実機では、Space でチェックを付け外しし、Enter はその質問をチェックのまま確定する。
func (a *adapter) ToggleKeys(s agent.Screen, n int) []agent.Key {
	_, glyphs, ok := a.readCard(s)
	if !ok || n < 1 || n > len(glyphs) || glyphs[n-1] != uncheckedGlyph {
		return nil
	}
	return []agent.Key{{Name: "Space"}}
}

// joinWrapped は折り返された行をつなぐ。折り返しの境目の両側が ASCII のときだけ空白を入れる（日本語は空白なしでつながる）。
func joinWrapped(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		if b.Len() > 0 {
			last, _ := utf8.DecodeLastRuneInString(b.String())
			first, _ := utf8.DecodeRuneInString(p)
			if last < utf8.RuneSelf && first < utf8.RuneSelf {
				b.WriteByte(' ')
			}
		}
		b.WriteString(p)
	}
	return b.String()
}

// SubmitFocused は質問カードのカーソルが Submit の行にあるか（agent.Submitter）。
// v19 は選択肢を選ぶ Enter のあとにカーソルが Submit へ移り、もう 1 回 Enter を送るまで回答は確定しない。
func (a *adapter) SubmitFocused(s agent.Screen) bool {
	lines := agent.TrimScreen(s.Lines)
	span, ok := a.liveCard(lines)
	if !ok {
		return false
	}
	for _, l := range lines[span.top+1 : span.bottom] {
		if inner, boxed := agent.BoxInner(l); boxed && strings.TrimSpace(inner) == submitFocused {
			return true
		}
	}
	return false
}

// PickKeys は n 番へカーソルを動かす Up/Down を差分だけ返す（Enter は含めない）。
func (a *adapter) PickKeys(c agent.Card, n int) ([]agent.Key, error) {
	if err := agent.CheckPick(c, n); err != nil {
		return nil, err
	}
	return agent.ArrowKeys(c.Cursor, n)
}

// CustomKeys は「Other (type your own)」へカーソルを動かす Down を返す。
func (a *adapter) CustomKeys(c agent.Card) ([]agent.Key, error) {
	if !c.AllowsCustom {
		return nil, apperr.New(apperr.CustomNotAllowed)
	}
	return agent.ArrowKeys(c.Cursor, len(c.Options)+1)
}
