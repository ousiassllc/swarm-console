package tomledit

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// find は path に完全に一致するキー（[[...]] の中は除く）を返す。
func (d *document) find(path []string) []entry {
	var out []entry
	for _, e := range d.entries {
		if !e.inArray && equalPath(e.path, path) {
			out = append(out, e)
		}
	}
	return out
}

// group は同じ表に足すキーの行。
type group struct {
	header int // 既にある表ヘッダーの番号。新しい表なら -1
	path   []string
	lines  []string
}

// plan は ops を splice の列にする。
func plan(d *document, ops []Op) ([]splice, error) {
	eol := detectEOL(d.src)
	var (
		sp        []splice
		rootLines []string
		groups    []*group
		seen      = map[string]bool{}
	)
	for _, op := range ops {
		if len(op.Key) == 0 || slicesContainsEmpty(op.Key) {
			return nil, errors.New("tomledit: キーが空です")
		}
		id := strings.Join(op.Key, "\x00")
		if seen[id] {
			return nil, fmt.Errorf("tomledit: %s が重複しています", strings.Join(op.Key, "."))
		}
		seen[id] = true
		text, err := encode(op.Value)
		if err != nil {
			return nil, err
		}
		matches := d.find(op.Key)
		switch {
		case len(matches) > 1:
			return nil, &PlacementError{Key: op.Key, Reason: "同じキーが 2 回以上書かれています"}
		case len(matches) == 1:
			e := matches[0]
			if e.comments && bytes.IndexByte(d.src[e.valStart:e.valEnd], '\n') >= 0 && !op.AllowCommentLoss {
				return nil, &CommentLossError{Key: op.Key}
			}
			sp = append(sp, splice{Off: e.valStart, Del: e.valEnd - e.valStart, Ins: text})
			continue
		}
		line := encodeKey(op.Key[len(op.Key)-1]) + " = " + text
		table := op.Key[:len(op.Key)-1]
		if len(table) == 0 {
			rootLines = append(rootLines, line)
			continue
		}
		hi, err := d.locate(op.Key, table)
		if err != nil {
			return nil, err
		}
		var g *group
		for _, c := range groups {
			if equalPath(c.path, table) {
				g = c
			}
		}
		if g == nil {
			g = &group{header: hi, path: table}
			groups = append(groups, g)
		}
		g.lines = append(g.lines, line)
	}
	ins := inserter{d: d, eol: eol}
	if len(rootLines) > 0 {
		ins.add(d.rootPoint(), rootLines)
	}
	for _, g := range groups {
		if g.header >= 0 {
			ins.add(d.tableEnd(g.header), g.lines)
		}
	}
	for _, g := range groups {
		if g.header < 0 {
			ins.addTable(g)
		}
	}
	sp = append(sp, ins.splices()...)
	if err := sortSplices(sp); err != nil {
		return nil, err
	}
	return sp, nil
}

func slicesContainsEmpty(s []string) bool {
	for _, k := range s {
		if k == "" {
			return true
		}
	}
	return false
}

// locate は key を足す表 table のヘッダー番号を返す。ヘッダーが無く新しく作れるなら -1。
// 足す先を決められない形（ER §5.1）は *PlacementError。
func (d *document) locate(key, table []string) (int, error) {
	fail := func(reason string) (int, error) { return 0, &PlacementError{Key: key, Reason: reason} }
	for _, h := range d.headers {
		if h.array && hasPrefix(table, h.path) {
			return fail("[[...]] の中です")
		}
	}
	hi := -1
	for i, h := range d.headers {
		if equalPath(h.path, table) {
			if hi >= 0 {
				return fail("同じ表が離れて 2 回以上出てきます")
			}
			hi = i
		}
	}
	for _, e := range d.entries {
		switch {
		case hasPrefix(e.path, table) && !hasPrefix(e.table, table):
			return fail("dotted key で別の表に分かれています")
		case hi < 0 && hasPrefix(table, e.path):
			return fail("インラインテーブルなどで書かれています")
		}
	}
	return hi, nil
}

// rootPoint は最上位のキーを足す位置。最初の表ヘッダーの直前（直上に続くコメント行は表の説明として動かさず、
// その前の空行は保つ）。表が 1 つも無ければ末尾。
func (d *document) rootPoint() int {
	if len(d.headers) == 0 {
		return len(d.src)
	}
	p := d.headers[0].lineStart
	for p > 0 {
		s := d.lineStartBefore(p)
		if !bytes.HasPrefix(bytes.TrimSpace(d.src[s:p]), []byte("#")) {
			break
		}
		p = s
	}
	for p > 0 {
		s := d.lineStartBefore(p)
		if len(bytes.TrimSpace(d.src[s:p])) != 0 {
			break
		}
		p = s
	}
	return p
}

// lineStartBefore は p（行頭）の 1 つ前の行の先頭。
func (d *document) lineStartBefore(p int) int {
	return bytes.LastIndexByte(d.src[:p-1], '\n') + 1
}

// tableEnd は表 hi の最後のキーの次の行の先頭（キーが無ければヘッダーの次の行）。
func (d *document) tableEnd(hi int) int {
	end := d.headers[hi].lineEnd
	for _, e := range d.entries {
		if e.section == hi && e.lineEnd > end {
			end = e.lineEnd
		}
	}
	return end
}

// detectEOL は最初の改行で LF・CRLF を決める（改行が無ければ LF）。
func detectEOL(src []byte) string {
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// inserter は挿入の splice を作る。末尾への挿入は 1 つにまとめ、末尾の改行の有無に合わせる。
type inserter struct {
	d    *document
	eol  string
	out  []splice
	tail []string
}

func (in *inserter) add(off int, lines []string) {
	if off >= len(in.d.src) {
		in.tail = append(in.tail, lines...)
		return
	}
	in.out = append(in.out, splice{Off: off, Ins: strings.Join(lines, in.eol) + in.eol})
}

// addTable は無い表を末尾に、空行 1 つ・ヘッダー・キーの順で足す。
func (in *inserter) addTable(g *group) {
	blank := len(in.tail) > 0 && in.tail[len(in.tail)-1] != "" || len(in.tail) == 0 && !in.d.endsBlank()
	if blank {
		in.tail = append(in.tail, "")
	}
	in.tail = append(in.tail, "["+encodePath(g.path)+"]")
	in.tail = append(in.tail, g.lines...)
}

func (in *inserter) splices() []splice {
	out := in.out
	if len(in.tail) == 0 {
		return out
	}
	text := strings.Join(in.tail, in.eol)
	if n := len(in.d.src); n > 0 && in.d.src[n-1] != '\n' {
		text = in.eol + text // 末尾の改行が無いファイルは、最後の行も改行なしのまま
	} else {
		text += in.eol
	}
	return append(out, splice{Off: len(in.d.src), Ins: text})
}

// endsBlank は末尾が空行（または空ファイル）で終わっているか。
func (d *document) endsBlank() bool {
	s := d.src
	if len(s) == 0 {
		return true
	}
	if s[len(s)-1] != '\n' {
		return false
	}
	s = s[:len(s)-1]
	return len(bytes.TrimSpace(s[bytes.LastIndexByte(s, '\n')+1:])) == 0
}
