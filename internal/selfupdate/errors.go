package selfupdate

import "errors"

// Failure は入れ替えに至らなかったことを表す（Reason だけを持つ。FR-18）。
type Failure struct{ Reason Reason }

func (f *Failure) Error() string { return "自己更新できません: " + string(f.Reason) }

func fail(r Reason) error { return &Failure{Reason: r} }

// ErrNotNewer は最新版が現在の版以下（ダウングレード・同じ版）で入れ替えないことを表す。apply の状況は変えない。
var ErrNotNewer = errors.New("最新版が現在の版より新しくないため入れ替えません")

// ErrBlocked は最新版が .update-failed の版以下で入れ替えないことを表す（apply は blocked）。
var ErrBlocked = errors.New("戻した版以下のため入れ替えません")

// ErrInvalidVersion は最新版が vX.Y.Z の形でないことを表す（呼び出し側の誤り。通信も書き込みもしない）。
var ErrInvalidVersion = errors.New("版の形が不正です")
