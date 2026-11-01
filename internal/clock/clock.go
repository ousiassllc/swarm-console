// Package clock は時計のインターフェースを定める。テストでは固定・手動進行の実装に差し替える。
package clock

import "time"

// Clock は現在時刻の取得と待機を抽象化する。
type Clock interface {
	Now() time.Time
	// After は d 経過後に現在時刻を送るチャネルを返す。
	After(d time.Duration) <-chan time.Time
}

// Real は標準の time パッケージを使う Clock。
type Real struct{}

// Now は現在時刻を返す。
func (Real) Now() time.Time { return time.Now() }

// After は time.After に委譲する。
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }
