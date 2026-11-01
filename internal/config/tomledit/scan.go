package tomledit

import (
	"bytes"
	"fmt"

	"github.com/pelletier/go-toml/v2/unstable"
)

// entry は `key = value` 1 つ。位置はすべて元のバイト列の先頭からのオフセット。
type entry struct {
	path     []string // 表のパス＋キー（dotted key を含む完全なキー）
	table    []string // 属する表ヘッダーのパス（最上位は空）
	section  int      // 属する表ヘッダーの番号（最上位は -1）
	inArray  bool     // [[...]] の中（キーの一致には使わない）
	valStart int
	valEnd   int
	lineEnd  int // このキーの行の次の行の先頭（行末の改行の後。改行が無ければ len(src)）
	comments bool
}

// header は [表] または [[表]] の行。
type header struct {
	path      []string
	array     bool
	lineStart int
	lineEnd   int
}

type document struct {
	src     []byte
	entries []entry
	headers []header
}

// scan は unstable.Parser で src を 1 度読み、キーの値のバイト範囲と表ヘッダーの位置を取る。値の解釈はしない。
func scan(src []byte) (*document, error) {
	var p unstable.Parser
	p.KeepComments = true
	p.Reset(src)
	d := &document{src: src}
	section := -1
	var table []string
	inArray := false
	for p.NextExpression() {
		n := p.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			path, first, last := keyPath(n.Key())
			start := first
			for start > 0 && bytes.IndexByte([]byte(" \t["), src[start-1]) >= 0 {
				start--
			}
			d.headers = append(d.headers, header{
				path:      path,
				array:     n.Kind == unstable.ArrayTable,
				lineStart: bytes.LastIndexByte(src[:start], '\n') + 1,
				lineEnd:   lineEnd(src, last),
			})
			section, table, inArray = len(d.headers)-1, path, n.Kind == unstable.ArrayTable
		case unstable.KeyValue:
			keys, _, last := keyPath(n.Key())
			v := n.Value()
			e := entry{
				path:     append(append([]string(nil), table...), keys...),
				table:    table,
				section:  section,
				inArray:  inArray,
				valStart: int(v.Raw.Offset),
				valEnd:   int(n.Raw.Offset + n.Raw.Length),
				comments: hasComment(v),
			}
			if v.Raw.Length == 0 { // 配列・インラインテーブルは値の先頭が Raw に入らない
				e.valStart = valueStart(src, last)
			}
			e.lineEnd = lineEnd(src, e.valEnd)
			d.entries = append(d.entries, e)
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("tomledit: TOML を読めません: %w", err)
	}
	return d, nil
}

// keyPath はキーの各部分（dotted key は複数）と、最初の部分の開始・最後の部分の終了のオフセットを返す。
func keyPath(it unstable.Iterator) (path []string, first, last int) {
	for it.Next() {
		k := it.Node()
		if path == nil {
			first = int(k.Raw.Offset)
		}
		path = append(path, string(k.Data))
		last = int(k.Raw.Offset + k.Raw.Length)
	}
	return path, first, last
}

// valueStart は最後のキーの終わりから `=` と空白を飛ばした位置（値の先頭）。
func valueStart(src []byte, keyEnd int) int {
	i := skipBlank(src, keyEnd)
	if i < len(src) && src[i] == '=' {
		i = skipBlank(src, i+1)
	}
	return i
}

func skipBlank(src []byte, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i
}

// lineEnd は from を含む行の次の行の先頭（改行が無ければ len(src)）。
func lineEnd(src []byte, from int) int {
	if i := bytes.IndexByte(src[from:], '\n'); i >= 0 {
		return from + i + 1
	}
	return len(src)
}

// hasComment は値の中（配列・インラインテーブルの入れ子を含む）にコメントがあるか。
func hasComment(n *unstable.Node) bool {
	for c := n.Child(); c != nil; c = c.Next() {
		if c.Kind == unstable.Comment || hasComment(c) {
			return true
		}
	}
	return false
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// hasPrefix は path が prefix で始まるか（等しい場合も true）。
func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && equalPath(path[:len(prefix)], prefix)
}
