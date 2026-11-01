// Package tomledit は config.toml を部分編集する純関数を持つ（I/O なし。ER §5.1）。
//
// 変えるキーの値のバイト範囲だけを置き換え、無いキーは決まった位置に 1 行足す。
// コメント・空行・並び・改行（LF・CRLF）・書式・知らないキーは 1 バイトも変えない。
// ファイル全体の再出力（Marshal）はしない。
package tomledit

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Op は 1 つのキーへの書き込み。Key は表を含む完全なキー（["agents","omp","command"]）。
// Value は string・[]string・bool・int。
type Op struct {
	Key   []string
	Value any
	// AllowCommentLoss は、コメント入りの複数行配列を 1 行に置き換えて中のコメントが消えることを承知している印（P-34）。
	// false のままコメント入りの配列を置き換えようとすると *CommentLossError を返す。
	AllowCommentLoss bool
}

// PlacementError は、挿入先を決められない形（インラインテーブルへの追加、同じ表が離れて 2 回出てくる、
// dotted key で別の表に分かれている など）のとき返す。何も書かず、該当キーを手で直してもらう。
type PlacementError struct {
	Key    []string
	Reason string
}

func (e *PlacementError) Error() string {
	return fmt.Sprintf("tomledit: %s を足す場所を決められません（%s）。手で書いてください", strings.Join(e.Key, "."), e.Reason)
}

// CommentLossError は、コメント入りの複数行配列を置き換えるとコメントが消える（AllowCommentLoss が false）。
type CommentLossError struct{ Key []string }

func (e *CommentLossError) Error() string {
	return fmt.Sprintf("tomledit: %s の配列の中のコメントが消えます", strings.Join(e.Key, "."))
}

// Edit は src に ops を適用した結果を返す。src は変えない。
// ops の順に、既にあるキーは値の範囲だけを置き換え、無いキーは足す。
// 返す前に、(1) 置き換えた範囲・足した行の外のバイト列が src と同一、(2) 結果が TOML として読めて書いた値が
// 期待どおり、を確かめ、崩れていればエラーにする（部分的な結果は返さない）。
func Edit(src []byte, ops []Op) ([]byte, error) {
	d, err := scan(src)
	if err != nil {
		return nil, err
	}
	sp, err := plan(d, ops)
	if err != nil {
		return nil, err
	}
	out := apply(src, sp)
	if err := verifyOutside(src, out, sp); err != nil {
		return nil, err
	}
	if err := verifyValues(out, ops); err != nil {
		return nil, err
	}
	return out, nil
}

// CommentedArrays は keys のうち、src に複数行の配列として書かれていて中にコメントがあるものを返す。
// これを置き換えるとコメントが消えるので、呼び出し側は確認してから AllowCommentLoss を付ける。
func CommentedArrays(src []byte, keys [][]string) ([][]string, error) {
	d, err := scan(src)
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, k := range keys {
		for _, e := range d.find(k) {
			if e.comments && bytes.IndexByte(src[e.valStart:e.valEnd], '\n') >= 0 {
				out = append(out, k)
			}
		}
	}
	return out, nil
}

// splice は元のバイト列の [Off, Off+Del) を Ins に置き換える（Del が 0 なら挿入）。
type splice struct {
	Off, Del int
	Ins      string
}

func apply(src []byte, sp []splice) []byte {
	var b bytes.Buffer
	pos := 0
	for _, s := range sp {
		b.Write(src[pos:s.Off])
		b.WriteString(s.Ins)
		pos = s.Off + s.Del
	}
	b.Write(src[pos:])
	return b.Bytes()
}

// verifyOutside は、splice の外側のバイト列が src と out で同一であることを確かめる。
func verifyOutside(src, out []byte, sp []splice) error {
	pos, outPos := 0, 0
	for _, s := range sp {
		n := s.Off - pos
		if outPos+n > len(out) || !bytes.Equal(src[pos:s.Off], out[outPos:outPos+n]) {
			return errors.New("tomledit: 変更範囲の外のバイト列が変わっています")
		}
		outPos += n + len(s.Ins)
		pos = s.Off + s.Del
	}
	if !bytes.Equal(src[pos:], out[min(outPos, len(out)):]) {
		return errors.New("tomledit: 変更範囲の外のバイト列が変わっています")
	}
	return nil
}

// verifyValues は out が TOML として読め、ops の値がそのとおりに読めることを確かめる。
func verifyValues(out []byte, ops []Op) error {
	var doc map[string]any
	if err := toml.Unmarshal(out, &doc); err != nil {
		return fmt.Errorf("tomledit: 結果が TOML として読めません: %w", err)
	}
	for _, op := range ops {
		var cur any = doc
		for _, k := range op.Key {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = m[k]
		}
		if !sameValue(cur, op.Value) {
			return fmt.Errorf("tomledit: %s の値が期待と違います", strings.Join(op.Key, "."))
		}
	}
	return nil
}

func sameValue(got, want any) bool {
	switch w := want.(type) {
	case string:
		g, ok := got.(string)
		return ok && g == w
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case int:
		g, ok := got.(int64)
		return ok && g == int64(w)
	case []string:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if s, ok := g[i].(string); !ok || s != w[i] {
				return false
			}
		}
		return true
	}
	return false
}

func sortSplices(sp []splice) error {
	sort.SliceStable(sp, func(i, j int) bool { return sp[i].Off < sp[j].Off })
	for i := 1; i < len(sp); i++ {
		if sp[i].Off < sp[i-1].Off+sp[i-1].Del {
			return errors.New("tomledit: 変更範囲が重なっています")
		}
	}
	return nil
}
