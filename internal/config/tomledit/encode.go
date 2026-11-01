package tomledit

import (
	"fmt"
	"strconv"
	"strings"
)

// encode は値を TOML の 1 行の書き方にする。文字列は基本文字列（"..."）、配列は 1 行。
func encode(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return quote(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case []string:
		parts := make([]string, len(x))
		for i, s := range x {
			parts[i] = quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	return "", fmt.Errorf("tomledit: 書けない型の値です: %T", v)
}

// quote は TOML の基本文字列にする（制御文字・"・\ をエスケープ）。
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// encodeKey はキーの 1 部分を書く。英数字・_・- だけなら素のまま、それ以外は基本文字列。
func encodeKey(k string) string {
	if k != "" && strings.IndexFunc(k, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-'
	}) < 0 {
		return k
	}
	return quote(k)
}

func encodePath(path []string) string {
	parts := make([]string, len(path))
	for i, k := range path {
		parts[i] = encodeKey(k)
	}
	return strings.Join(parts, ".")
}
