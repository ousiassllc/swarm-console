package omp

import (
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/agent/omp/transcript"
)

// 質問カードの選択肢の記号（実画面）。カーソルのある単一選択は U+F192、ほかの単一選択は U+F10C。複数選択は uncheckedGlyph（card.go）。
const (
	selectedRadioGlyph = '\uf192'
	radioGlyph         = '\uf10c'
)

// askCard は質問 q を、omp v19 の質問カードの形（`╭─ Ask` の枠、見出し、選択肢、Submit の行）の行にする。
// カーソルは recommended の選択肢（omp が最初にカーソルを合わせる位置）。「Other: type your own」の行は常に付く。
func askCard(q transcript.Question) []string {
	cursor := min(max(q.Recommended, 0), max(len(q.Options)-1, 0))
	rows := []string{boxRow(q.Question)}
	for i, label := range q.Options {
		if q.HasRecommended && i == q.Recommended && !recommendedRE.MatchString(label) {
			label += " (recommended)"
		}
		rows = append(rows, boxRow(optionLine(i == cursor, q.Multi, label)))
	}
	rows = append(rows, boxRow(optionLine(false, q.Multi, "Other: type your own")), boxRow(""), boxRow("  Submit"))
	top := "╭─ Ask " + strings.Repeat("─", max(nativeBoxWidth-len("╭─ Ask ")-1, 1)) + "╮"
	bottom := "╰" + strings.Repeat("─", nativeBoxWidth-2) + "╯"
	return append(append([]string{top}, rows...), bottom)
}

// optionLine は選択肢の 1 行（枠の中身）。カーソルのある行は行頭に cursorGlyph。
func optionLine(cursor, multi bool, label string) string {
	glyph := radioGlyph
	switch {
	case multi:
		glyph = uncheckedGlyph
	case cursor:
		glyph = selectedRadioGlyph
	}
	lead := " "
	if cursor {
		lead = string(cursorGlyph)
	}
	return lead + " " + string(glyph) + " " + label
}

// boxRow は枠の中の 1 行（`│ 中身 │`）。
func boxRow(content string) string {
	inner := nativeBoxWidth - 4
	if pad := inner - utf8.RuneCountInString(content); pad > 0 {
		content += strings.Repeat(" ", pad)
	}
	return "│ " + content + " │"
}
