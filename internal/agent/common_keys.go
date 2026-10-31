package agent

import (
	"strconv"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// CheckPick は n が選択肢の範囲（1..len）にあるか確かめる。
func CheckPick(c Card, n int) error {
	if n < 1 || n > len(c.Options) {
		return apperr.New(apperr.OptionOutOfRange, "n", n, "options", len(c.Options))
	}
	return nil
}

// ArrowKeys はカーソルを from から to へ動かす Up/Down を差分だけ返す（Enter は含めない）。
// from が 1 未満（カーソルが読めない）ときは cursor_mismatch。
func ArrowKeys(from, to int) ([]Key, error) {
	if from < 1 || to < 1 {
		return nil, apperr.New(apperr.CursorMismatch, "reason", "cursor_unknown")
	}
	name := "Down"
	steps := to - from
	if steps < 0 {
		name, steps = "Up", -steps
	}
	keys := make([]Key, steps)
	for i := range keys {
		keys[i] = Key{Name: name}
	}
	return keys, nil
}

// DigitKey は数字キー（1〜9）を返す。範囲外は option_out_of_range。
func DigitKey(n int) (Key, error) {
	if n < 1 || n > 9 {
		return Key{}, apperr.New(apperr.OptionOutOfRange, "n", n)
	}
	return Key{Name: strconv.Itoa(n)}, nil
}

// ArrowOrDigit はカーソルが読めるときは Up/Down の差分、読めないときは数字キーで to を選ぶ。
func ArrowOrDigit(cursor, to int) ([]Key, error) {
	if cursor >= 1 {
		return ArrowKeys(cursor, to)
	}
	k, err := DigitKey(to)
	if err != nil {
		return nil, err
	}
	return []Key{k}, nil
}
