package render

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCell(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty", "", "—"},
		{"spaces", " \t\n ", "—"},
		{"pipe", "a|b", `a\|b`},
		{"newline", "a\nb\r\nc", "a b  c"},
		{"control", "a\x1b[31mb\x00", "a [31mb"},
		{"invalid utf8", "a\xffb", "a\uFFFDb"},
		{"plain", "Running go test ./...", "Running go test ./..."},
	}
	for _, c := range cases {
		if got := cell(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestCellTruncation(t *testing.T) {
	for _, unit := range []string{"a", "あ", "😀", "é"} {
		at80 := strings.Repeat(unit, 80)
		if got := cell(at80); got != at80 {
			t.Errorf("%q 80 字ちょうどは切らない", unit)
		}
		got := cell(strings.Repeat(unit, 81))
		if utf8.RuneCountInString(got) != 80 || !strings.HasSuffix(got, "…") || got != strings.Repeat(unit, 79)+"…" {
			t.Errorf("%q 81 字は 79 字＋…: %q", unit, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%q 切った結果が不正な UTF-8", unit)
		}
	}
	// エスケープは切った後にかかる（80 字の上限にバックスラッシュを数えない）
	if got := cell(strings.Repeat("|", 80)); got != strings.Repeat(`\|`, 80) {
		t.Errorf("| 80 個: %q", got)
	}
	// 切り詰め前に空白を整える: 前後の空白は字数に数えない
	if got := cell("  " + strings.Repeat("x", 80) + "  "); got != strings.Repeat("x", 80) {
		t.Errorf("前後の空白: %q", got)
	}
}

func TestFence(t *testing.T) {
	if got := fence([]string{"a", "b"}); got != "```\na\nb\n```" {
		t.Fatalf("%q", got)
	}
	if got := fence([]string{"x ```` y"}); !strings.HasPrefix(got, "`````\n") {
		t.Fatalf("本文より長い囲みにする: %q", got)
	}
}

func TestBlockIndentsContinuation(t *testing.T) {
	if got := block("a\r\nb\rc\x00d"); got != "a\n  b\n  c d" {
		t.Fatalf("%q", got)
	}
	if block("  ") != "—" {
		t.Fatal("空は —")
	}
}
