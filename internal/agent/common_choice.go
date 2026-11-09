package agent

import (
	"regexp"
	"strings"
)

// Choice はカーソル記号（`❯`・`›`）つきの選択肢の並び（Claude Code・Codex の確認カード）。
type Choice struct {
	Start, End int      // lines[Start:End] が選択肢（説明の続き行を含む）
	Labels     []string // 選択肢の文言（番号・カーソル記号を除く）
	Cursor     int      // カーソルのある選択肢（1 始まり）
	Numbered   bool     // `1.` `2.` の番号つきか
}

const (
	choiceWindow = 30 // 画面の下からこの行数だけ探す
	choiceAfter  = 4  // 選択肢の後ろに許す非空行の数（フッターなど）
)

var numberedRE = regexp.MustCompile(`^\s*(?:[❯›][\s\x{a0}]+)?\d+\.[\s\x{a0}]+(\S.*?)\s*$`)

// FindChoice は画面の下のほうにあるカーソル記号つきの選択肢の並びを探す。
// skip(i) が true の行は（入力欄など）カーソル行として扱わない。2 件未満・後ろに行が多すぎるものは対象外。
func FindChoice(lines []string, cursors string, skip func(i int) bool) (Choice, bool) {
	mark := `[` + regexp.QuoteMeta(cursors) + `]`
	cursorRE := regexp.MustCompile(`^\s*` + mark + `[\s\x{a0}]+\S`)
	plainRE := regexp.MustCompile(`^ {1,4}(?:` + mark + `[\s\x{a0}]+)?(\S.*?)\s*$`)
	cur := -1
	for i := len(lines) - 1; i >= max(0, len(lines)-choiceWindow); i-- {
		if cursorRE.MatchString(lines[i]) && (skip == nil || !skip(i)) {
			cur = i
			break
		}
	}
	if cur < 0 {
		return Choice{}, false
	}
	ch := Choice{Numbered: numberedRE.MatchString(lines[cur])}
	label := func(l string) (string, bool) {
		re := plainRE
		if ch.Numbered {
			re = numberedRE
		}
		m := re.FindStringSubmatch(l)
		if m == nil || strings.Contains(l, "──") {
			return "", false
		}
		return m[1], true
	}
	first := cur
	for first > 0 {
		if _, ok := label(lines[first-1]); ok {
			first--
			continue
		}
		// 上の選択肢が長くて折り返したとき、その続き行（実画面の Codex の `(p)` だけの行）をまたいで上の選択肢へ続ける。
		j := first - 1
		for ch.Numbered && j >= 0 && strings.TrimSpace(lines[j]) != "" && Indent(lines[j]) >= 5 {
			if _, ok := label(lines[j]); ok {
				break
			}
			j--
		}
		if j == first-1 || j < 0 {
			break
		}
		if _, ok := label(lines[j]); !ok {
			break
		}
		first = j
	}
	end := cur + 1
	for end < len(lines) {
		if _, ok := label(lines[end]); ok {
			end++
			continue
		}
		if ch.Numbered && strings.TrimSpace(lines[end]) != "" && Indent(lines[end]) >= 5 {
			end++ // 説明の続き行
			continue
		}
		break
	}
	for i := first; i < end; i++ {
		if l, ok := label(lines[i]); ok {
			if i == cur {
				ch.Cursor = len(ch.Labels) + 1
			}
			ch.Labels = append(ch.Labels, l)
		}
	}
	ch.Start, ch.End = first, end
	rest := 0
	for _, l := range lines[end:] {
		if strings.TrimSpace(l) != "" {
			rest++
		}
	}
	return ch, len(ch.Labels) >= 2 && rest <= choiceAfter
}

var recommendedRE = regexp.MustCompile(`(?i)\s*\(recommended\)\s*$`)

// BuildCard は選択肢の並びから Card を作る。custom に当たる文言は「その他（自由入力）」として
// Options から外し AllowsCustom を立てる（カーソルがそこにあれば Cursor は len(Options)+1）。
// tidy は文言から表示用のヒント（`(y)` など）を除く（nil なら何もしない）。
func BuildCard(lines []string, ch Choice, custom *regexp.Regexp, tidy func(string) string) Card {
	c := Card{Question: QuestionAbove(lines, ch.Start)}
	for i, l := range ch.Labels {
		if tidy != nil {
			l = tidy(l)
		}
		if custom != nil && custom.MatchString(l) {
			c.AllowsCustom = true
			if i+1 == ch.Cursor {
				c.Cursor = len(c.Options) + 1
			}
			continue
		}
		rec := recommendedRE.MatchString(l)
		c.Options = append(c.Options, Option{N: len(c.Options) + 1, Label: recommendedRE.ReplaceAllString(l, ""), Recommended: rec})
		if i+1 == ch.Cursor {
			c.Cursor = len(c.Options)
		}
	}
	return c
}

// QuestionAbove は選択肢の上にある質問文を返す。空行で区切った段落（最大 12 行まで遡る）のうち、
// `?` を含む最も近い段落、なければ最も近い段落を 1 行にまとめて返す。
func QuestionAbove(lines []string, start int) string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, strings.Join(cur, " "))
			cur = nil
		}
	}
	for i := start - 1; i >= max(0, start-12); i-- {
		t := strings.Trim(lines[i], " \u00a0│")
		if t == "" || strings.Contains(t, "──") || strings.HasPrefix(t, "▔▔▔") {
			flush()
			continue
		}
		cur = append([]string{t}, cur...)
	}
	flush()
	for _, p := range paras {
		if strings.ContainsAny(p, "?？") {
			return p
		}
	}
	if len(paras) > 0 {
		return paras[0]
	}
	return ""
}
