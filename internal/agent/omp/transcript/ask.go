package transcript

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// Question は ask ツールの質問 1 つ。
type Question struct {
	Question    string
	Options     []string
	Multi       bool
	Recommended int // 最初にカーソルが合う選択肢の番号（0 始まり）
	// HasRecommended は recommended が指定されていること（questions の recommended、html の checked）。omp はその選択肢の文言に ` (recommended)` を足す。
	HasRecommended bool
}

// ParseAsk は ask ツールの引数（omp v19 は html のフォームか questions の配列）から質問を読む。読めなければ nil。
func ParseAsk(args json.RawMessage) []Question {
	var a struct {
		HTML      string `json:"html"`
		Questions []struct {
			Question    string            `json:"question"`
			Options     []json.RawMessage `json:"options"`
			Multi       bool              `json:"multi"`
			Recommended *int              `json:"recommended"`
		} `json:"questions"`
	}
	if json.Unmarshal(args, &a) != nil {
		return nil
	}
	if a.HTML != "" {
		return parseHTML(a.HTML)
	}
	var qs []Question
	for _, q := range a.Questions {
		aq := Question{Question: q.Question, Multi: q.Multi}
		for _, raw := range q.Options {
			var label string
			var obj struct {
				Label string `json:"label"`
			}
			if json.Unmarshal(raw, &label) != nil {
				_ = json.Unmarshal(raw, &obj)
				label = obj.Label
			}
			aq.Options = append(aq.Options, label)
		}
		if q.Recommended != nil {
			aq.Recommended, aq.HasRecommended = *q.Recommended, true
		}
		qs = append(qs, aq)
	}
	return qs
}

var (
	fieldsetRE = regexp.MustCompile(`(?is)<fieldset\b[^>]*>(.*?)</fieldset>`)
	legendRE   = regexp.MustCompile(`(?is)<legend\b[^>]*>(.*?)</legend>`)
	choiceRE   = regexp.MustCompile(`(?is)<label\b[^>]*>\s*<input\b([^>]*)>(.*?)</label>`)
	selectRE   = regexp.MustCompile(`(?is)<select\b[^>]*>(.*?)</select>`)
	optionTag  = regexp.MustCompile(`(?is)<option\b([^>]*)>(.*?)</option>`)
	typeAttrRE = regexp.MustCompile(`(?i)\btype\s*=\s*["']?(\w+)`)
	checkedRE  = regexp.MustCompile(`(?i)\bchecked\b|\bselected\b`)
	tagRE      = regexp.MustCompile(`(?s)<[^>]*>`)
)

// parseHTML は ask の html のフォームを、fieldset ごとの質問にする。fieldset が無ければフォーム全体で 1 問。
// radio・select は単一選択、checkbox は複数選択。checked の選択肢が recommended（カーソルの最初の位置）になる。
func parseHTML(src string) []Question {
	groups := []string{src}
	if m := fieldsetRE.FindAllStringSubmatch(src, -1); len(m) > 0 {
		groups = groups[:0]
		for _, g := range m {
			groups = append(groups, g[1])
		}
	}
	var qs []Question
	for _, g := range groups {
		var q Question
		if m := legendRE.FindStringSubmatch(g); m != nil {
			q.Question = htmlText(m[1])
		}
		for _, m := range choiceRE.FindAllStringSubmatch(g, -1) {
			if t := typeAttrRE.FindStringSubmatch(m[1]); t != nil && strings.EqualFold(t[1], "checkbox") {
				q.Multi = true
			}
			if checkedRE.MatchString(m[1]) && !q.HasRecommended {
				q.Recommended, q.HasRecommended = len(q.Options), true
			}
			q.Options = append(q.Options, htmlText(m[2]))
		}
		if sm := selectRE.FindStringSubmatch(g); sm != nil && len(q.Options) == 0 {
			for _, m := range optionTag.FindAllStringSubmatch(sm[1], -1) {
				if checkedRE.MatchString(m[1]) && !q.HasRecommended {
					q.Recommended, q.HasRecommended = len(q.Options), true
				}
				q.Options = append(q.Options, htmlText(m[2]))
			}
		}
		if len(q.Options) > 0 {
			qs = append(qs, q)
		}
	}
	return qs
}

// htmlText はタグを除き、空白をつめて、文字参照を戻した文字列にする。
func htmlText(s string) string {
	return html.UnescapeString(strings.Join(strings.Fields(tagRE.ReplaceAllString(s, " ")), " "))
}
