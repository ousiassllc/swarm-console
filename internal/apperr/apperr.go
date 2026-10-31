// Package apperr はツールが返すエラーコードと固定メッセージ（API 仕様 §3）を定める。
// メッセージは日本語の固定文で、どのクライアントからでも同じ文面になる。
package apperr

import (
	"errors"
	"fmt"
)

// Code は API 仕様 §3.2 のエラーコード。
type Code string

// エラーコード一覧（API 仕様 §3.2）。
const (
	Unauthorized         Code = "unauthorized"
	ForbiddenScope       Code = "forbidden_scope"
	NotFound             Code = "not_found"
	MethodNotAllowed     Code = "method_not_allowed"
	PayloadTooLarge      Code = "payload_too_large"
	UnknownTool          Code = "unknown_tool"
	InvalidArgs          Code = "invalid_args"
	PaneNotFound         Code = "pane_not_found"
	UnsupportedAgent     Code = "unsupported_agent"
	InvalidState         Code = "invalid_state"
	NotAsking            Code = "not_asking"
	OptionOutOfRange     Code = "option_out_of_range"
	CustomNotAllowed     Code = "custom_not_allowed"
	CursorMismatch       Code = "cursor_mismatch"
	ConfirmRequired      Code = "confirm_required"
	ForbiddenControlChar Code = "forbidden_control_char"
	TextTooLong          Code = "text_too_long"
	ForbiddenPath        Code = "forbidden_path"
	FileUnreadable       Code = "file_unreadable"
	SecretDetected       Code = "secret_detected"
	BusyTimeout          Code = "busy_timeout"
	TmuxError            Code = "tmux_error"
	Internal             Code = "internal"
)

// messages は固定の日本語メッセージ表。認証の失敗文を含むだけで秘密の値ではない。
var messages = map[Code]string{ //nolint:gosec // G101 の誤検知（unauthorized などのエラー文）
	Unauthorized:         "認証に失敗しました",
	ForbiddenScope:       "このトークンではこの窓口を使えません",
	NotFound:             "見つかりません",
	MethodNotAllowed:     "このメソッドは使えません",
	PayloadTooLarge:      "リクエスト本文が大きすぎます",
	UnknownTool:          "この窓口にないツールです",
	InvalidArgs:          "引数が正しくありません",
	PaneNotFound:         "ペインが見つかりません",
	UnsupportedAgent:     "このペインのエージェントは操作に対応していません",
	InvalidState:         "このペインの状態では操作できません",
	NotAsking:            "質問カードが出ていません",
	OptionOutOfRange:     "選択肢の番号が範囲外です",
	CustomNotAllowed:     "このカードは自由入力を受け付けません",
	CursorMismatch:       "カーソルが目的の位置に合わないため中止しました（Enter は送っていません）",
	ConfirmRequired:      "confirm: true が必要です",
	ForbiddenControlChar: "許可していない制御文字が含まれています",
	TextTooLong:          "文字数が上限（8000 字）を超えています",
	ForbiddenPath:        "許可されていないパスです",
	FileUnreadable:       "ファイルを読めません",
	SecretDetected:       "秘密に見える文字列が含まれているため拒否しました",
	BusyTimeout:          "同じペインの前の操作が終わりませんでした",
	TmuxError:            "tmux の呼び出しに失敗しました",
	Internal:             "内部エラーが発生しました",
}

// Message は code の固定メッセージを返す。未知のコードは Internal のメッセージ。
func Message(c Code) string {
	if m, ok := messages[c]; ok {
		return m
	}
	return messages[Internal]
}

// Error は API 仕様 §3.1 の {"error":{"code","message","details"}} に対応する。
// Details にはトークン・本文・メモの内容・秘密に見えた値を入れない。
type Error struct {
	Code    Code
	Details map[string]any
	Err     error // 内部原因（応答に出さない。ログ用）
}

// Error は errors.Error を実装する。
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

// Unwrap は内部原因を返す。
func (e *Error) Unwrap() error { return e.Err }

// Message は固定メッセージを返す。
func (e *Error) Message() string { return Message(e.Code) }

// New は details を key, value の交互列で受け取って Error を作る。
func New(code Code, kv ...any) *Error {
	e := &Error{Code: code}
	if len(kv) > 1 {
		e.Details = make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				e.Details[k] = kv[i+1]
			}
		}
	}
	return e
}

// Wrap は New に内部原因を付ける。
func Wrap(code Code, err error, kv ...any) *Error {
	e := New(code, kv...)
	e.Err = err
	return e
}

// As は err から *Error を取り出す。取れなければ Internal の *Error に包んで返す。
func As(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Wrap(Internal, err)
}
