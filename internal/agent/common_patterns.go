package agent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// PatternSrc は組み込み既定の判定パターン（正規表現の文字列）。
// config.toml の [agents.<name>] で書かれたキーだけが置き換わる（FR-AD-2）。
type PatternSrc struct {
	ProcessMatch []string // argv[0]（インタプリタ経由なら argv[1]）に当てる正規表現
	Working      string   // 作業中の目印の行
	Ask          string   // 質問カードの目印
	Idle         string   // 入力待ちの目印
	StatusLine   string   // ステータス行（Clean・StableText で除く）
	Border       string   // 枠線の行（Clean で除く）
	SpinnerChars string   // スピナーの文字（どれか 1 文字）
}

// Patterns はコンパイル済みの判定パターン（コンポーネント設計 §4）。
type Patterns struct {
	ProcessMatch []*regexp.Regexp
	Working      *regexp.Regexp
	Ask          *regexp.Regexp
	Idle         *regexp.Regexp
	StatusLine   *regexp.Regexp
	Border       *regexp.Regexp
	Spinner      *regexp.Regexp // SpinnerChars のどれか 1 文字
	SpinnerChars string
}

// Spec はアダプタの組み込み既定値。
type Spec struct {
	Name  string
	Start string // 起動コマンド
	Exit  string // 終了コマンド
	Src   PatternSrc
}

// Base は Name・StartCommand・ExitCommand・MatchProcess を持つ。各アダプタが埋め込む。
type Base struct {
	name, start, exit string
	P                 *Patterns
}

// NewBase は spec に ov を適用して Base を作る。ov で書かれたキーだけ置き換え、
// 正規表現が不正ならエラーを返す（空文字・nil は「上書きしない」）。
func NewBase(spec Spec, ov Overrides) (*Base, error) {
	src := spec.Src
	set := func(dst *string, v *string) {
		if v != nil && *v != "" {
			*dst = *v
		}
	}
	b := &Base{name: spec.Name, start: spec.Start, exit: spec.Exit}
	set(&b.start, ov.Command)
	set(&b.exit, ov.ExitCommand)
	if len(ov.ProcessMatch) > 0 {
		src.ProcessMatch = ov.ProcessMatch
	}
	set(&src.Working, ov.WorkingRE)
	set(&src.Ask, ov.AskRE)
	set(&src.Idle, ov.IdleRE)
	set(&src.StatusLine, ov.StatusLineRE)
	set(&src.Border, ov.BorderRE)
	set(&src.SpinnerChars, ov.SpinnerChars)
	p, err := compilePatterns(spec.Name, src)
	if err != nil {
		return nil, err
	}
	b.P = p
	return b, nil
}

func compilePatterns(name string, src PatternSrc) (*Patterns, error) {
	p := &Patterns{SpinnerChars: src.SpinnerChars}
	one := func(key, expr string, dst **regexp.Regexp) error {
		re, err := regexp.Compile(expr)
		if err != nil {
			return fmt.Errorf("agent %s: %s が正規表現として不正です: %w", name, key, err)
		}
		*dst = re
		return nil
	}
	for _, c := range []struct {
		key, expr string
		dst       **regexp.Regexp
	}{
		{"working_re", src.Working, &p.Working},
		{"ask_re", src.Ask, &p.Ask},
		{"idle_re", src.Idle, &p.Idle},
		{"status_line_re", src.StatusLine, &p.StatusLine},
		{"border_re", src.Border, &p.Border},
	} {
		if err := one(c.key, c.expr, c.dst); err != nil {
			return nil, err
		}
	}
	for _, expr := range src.ProcessMatch {
		var re *regexp.Regexp
		if err := one("process_match", expr, &re); err != nil {
			return nil, err
		}
		p.ProcessMatch = append(p.ProcessMatch, re)
	}
	var cls strings.Builder
	cls.WriteByte('[')
	for _, r := range src.SpinnerChars {
		fmt.Fprintf(&cls, `\x{%x}`, r)
	}
	cls.WriteByte(']')
	if src.SpinnerChars == "" {
		cls.Reset()
		cls.WriteString(`[^\s\S]`) // 何にも当たらない
	}
	return p, one("spinner_chars", cls.String(), &p.Spinner)
}

// Name はアダプタ名。
func (b *Base) Name() string { return b.name }

// StartCommand は起動コマンド。
func (b *Base) StartCommand() string { return b.start }

// ExitCommand は終了コマンド。
func (b *Base) ExitCommand() string { return b.exit }

// interpreters は argv[1] にスクリプト名が来るコマンド名。
var interpreters = map[string]bool{"node": true, "nodejs": true, "bun": true, "deno": true, "npx": true}

// MatchProcess は process_match のどれかが argv[0] に当たるか判定する。
// argv[0] がインタプリタ（node・bun など）のときは argv[1] も見る（`vim omp` のような引数の偶然の一致を避ける）。
func (b *Base) MatchProcess(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	targets := []string{argv[0]}
	if interpreters[filepath.Base(argv[0])] && len(argv) > 1 {
		targets = append(targets, argv[1])
	}
	for _, re := range b.P.ProcessMatch {
		for _, t := range targets {
			if re.MatchString(t) {
				return true
			}
		}
	}
	return false
}
