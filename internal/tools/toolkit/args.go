package toolkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// fieldName は details.field に載せてよい引数名の形。クライアントが打った未知キーをそのまま返さないための絞り込み。
var fieldName = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)

// InvalidArg は invalid_args（details.field に引数名だけ）を作る。field が "" なら details なし。
func InvalidArg(field string) *apperr.Error {
	if field == "" {
		return apperr.New(apperr.InvalidArgs)
	}
	return apperr.New(apperr.InvalidArgs, "field", field)
}

// DecodeArgs は引数 JSON を v（構造体へのポインタ）へ厳密に読む。
// 未知のキー・型違い・オブジェクトでない値・後続のゴミは invalid_args。空・null は {} として扱う。
func DecodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return InvalidArg(fieldOf(err))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return InvalidArg("")
	}
	return checkExactKeys(raw, v)
}

// checkExactKeys は encoding/json が大文字小文字を区別せずにキーを合わせる点を塞ぐ。
// 構造体の json タグと完全一致しないキーは未知のキーとして invalid_args。
func checkExactKeys(raw json.RawMessage, v any) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return InvalidArg("")
	}
	t := reflect.TypeOf(v).Elem()
	known := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" {
			known[name] = true
		}
	}
	for k := range keys {
		if known[k] {
			continue
		}
		if fieldName.MatchString(k) {
			return InvalidArg(k)
		}
		return InvalidArg("")
	}
	return nil
}

// fieldOf は JSON デコードの失敗から、引数名だけを取り出す（取れなければ ""）。
func fieldOf(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		name := te.Field
		if i := strings.IndexByte(name, '.'); i >= 0 {
			name = name[:i]
		}
		if fieldName.MatchString(name) {
			return name
		}
		return ""
	}
	const prefix = "json: unknown field "
	if msg := err.Error(); strings.HasPrefix(msg, prefix) {
		name := strings.Trim(msg[len(prefix):], `"`)
		if fieldName.MatchString(name) {
			return name
		}
	}
	return ""
}

// RequireStr は必須の文字列引数（空文字も不可）を取り出す。無ければ invalid_args。
func RequireStr(p *string, field string) (string, error) {
	if p == nil || *p == "" {
		return "", InvalidArg(field)
	}
	return *p, nil
}

// WithField は err が *apperr.Error なら details.field を足して返す。
func WithField(err error, field string) error {
	e := apperr.As(err)
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details["field"] = field
	return e
}
