package transcript

import (
	"bytes"
	"io"
	"os"
)

// 会話の記録（セッションの jsonl）の末尾から読む量の下限と上限（Issue #133・#149）。
// 1 行（ツールの結果）が数十〜百 KB になることがあるので、呼び出し側は TailMin から読み始め、足りなければ TailMax まで倍に広げる（GrowTail）。
// 下限は呼び出し元（session.go・reaction.go・Read）で共通。
const (
	TailMin = 256 << 10
	TailMax = 2 << 20
)

// ReadTail は f（大きさ size）の末尾 n バイトを返す。途中から読み始めたときは、切れた最初の行を捨てる。
// 途中から読み始めて改行が 1 つも無い（1 行が n バイトより大きい）ときは nil を返す。
func ReadTail(f *os.File, size, n int64) ([]byte, error) {
	off := max(size-n, 0)
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil, err
	}
	if off > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return nil, nil
		}
		buf = buf[i+1:]
	}
	return buf, nil
}

// GrowTail は f の末尾 minSize バイトから読み始め、fn が done=false を返す間、窓を倍にして maxSize まで広げて fn を呼び直す。
// buf は窓の中の完全な行（切れた先頭行は捨てたもの。1 行が窓より大きければ空）。atStart は窓がファイルの先頭に届いたこと。
// done=true・窓が maxSize に達した・窓がファイル全体になった、のどれかで終わる。読み込みに失敗したときは err を返す。
func GrowTail(f *os.File, minSize, maxSize int64, fn func(buf []byte, atStart bool) (done bool)) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	size := st.Size()
	for n := minSize; ; n *= 2 {
		n = min(n, maxSize)
		buf, err := ReadTail(f, size, n)
		if err != nil {
			return err
		}
		if fn(buf, n >= size) || n >= maxSize || n >= size {
			return nil
		}
	}
}
