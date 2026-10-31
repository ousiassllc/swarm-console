package safety

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

func TestCheckTextControlChars(t *testing.T) {
	p := NewPolicy(nil2())
	// 許可: 通常の文字・改行・タブ・日本語・絵文字・書式文字。
	for _, s := range []string{"", "hello", "a\nb\tc", "日本語のテキスト\n次の行", "😀 emoji", "a\u200bb", "\u2028", "```go\nfmt.Println()\n```"} {
		if err := p.CheckText(s); err != nil {
			t.Errorf("%q は許可のはず: %v", s, err)
		}
	}
	// 拒否: \n・\t 以外の C0 全種、DEL、C1 全種。
	var bad []rune
	for r := rune(0); r < 0x20; r++ {
		if r != '\n' && r != '\t' {
			bad = append(bad, r)
		}
	}
	bad = append(bad, 0x7f)
	for r := rune(0x80); r <= 0x9f; r++ {
		bad = append(bad, r)
	}
	for _, r := range bad {
		for _, s := range []string{string(r), "ok" + string(r), string(r) + "ok", "a\n" + string(r) + "\tb"} {
			err := p.CheckText(s)
			if code(err) != apperr.ForbiddenControlChar {
				t.Errorf("U+%04X を含む %q: forbidden_control_char のはずが %v", r, s, err)
			}
		}
	}
	// bracketed paste の終端の偽装。
	if err := p.CheckText("x\x1b[201~rm -rf /"); code(err) != apperr.ForbiddenControlChar {
		t.Errorf("ESC[201~: %v", err)
	}
	// 不正な UTF-8（8 ビットの C1 を含む生バイト）。
	for _, s := range []string{"\x9b", "a\xffb", "\xc3"} {
		if err := p.CheckText(s); code(err) != apperr.ForbiddenControlChar {
			t.Errorf("不正 UTF-8 %q: %v", s, err)
		}
	}
	// details に文字の内容は入れず、コードポイントだけ。
	ae := apperr.As(p.CheckText("secret\x1btext"))
	if fmt.Sprint(ae.Details) != "map[codepoint:U+001B]" {
		t.Errorf("details = %v", ae.Details)
	}
}

func TestCheckTextLength(t *testing.T) {
	p := NewPolicy(nil2())
	tests := []struct {
		name string
		s    string
		want apperr.Code
	}{
		{"8000 字ちょうど", strings.Repeat("a", 8000), ""},
		{"8001 字", strings.Repeat("a", 8001), apperr.TextTooLong},
		{"日本語 8000 字（24000 バイト）", strings.Repeat("あ", 8000), ""},
		{"日本語 8001 字", strings.Repeat("あ", 8001), apperr.TextTooLong},
		{"絵文字 8000 字", strings.Repeat("😀", 8000), ""},
		{"絵文字 8001 字", strings.Repeat("😀", 8001), apperr.TextTooLong},
		{"改行を含めて数える", strings.Repeat("\n", 8001), apperr.TextTooLong},
	}
	for _, tt := range tests {
		if got := code(p.CheckText(tt.s)); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	if ae := apperr.As(p.CheckText(strings.Repeat("a", 8001))); ae.Details["max"] != 8000 {
		t.Errorf("details = %v", ae.Details)
	}
}

func TestCheckLine(t *testing.T) {
	p := NewPolicy(nil2())
	tests := []struct {
		name string
		s    string
		max  int
		want apperr.Code
	}{
		{"1 行", "CI 待ち", 500, ""},
		{"空", "", 500, ""},
		{"タブは可", "a\tb", 500, ""},
		{"ちょうど max", strings.Repeat("あ", 500), 500, ""},
		{"max 超え", strings.Repeat("あ", 501), 500, apperr.TextTooLong},
		{"改行", "a\nb", 500, apperr.ForbiddenControlChar},
		{"末尾の改行", "a\n", 500, apperr.ForbiddenControlChar},
		{"CR", "a\rb", 500, apperr.ForbiddenControlChar},
		{"ESC", "a\x1bb", 500, apperr.ForbiddenControlChar},
		{"NUL", "\x00", 500, apperr.ForbiddenControlChar},
		{"C1", "a\u0085b", 500, apperr.ForbiddenControlChar},
		{"不正 UTF-8", "a\xff", 500, apperr.ForbiddenControlChar},
		{"制御文字と長さ両方なら制御文字", strings.Repeat("a", 600) + "\n", 500, apperr.ForbiddenControlChar},
	}
	for _, tt := range tests {
		if got := code(p.CheckLine(tt.s, tt.max)); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	if ae := apperr.As(p.CheckLine(strings.Repeat("a", 501), 500)); ae.Details["max"] != 500 {
		t.Errorf("details = %v", ae.Details)
	}
}
