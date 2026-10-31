package tools

import (
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// maxDetailRunes は details の文字列値として許す最大字数。
const maxDetailRunes = 100

type errorBody struct {
	Error errorObject `json:"error"`
}

type errorObject struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// ErrorText は err を API 仕様 §3.1 の本文 {"error":{"code","message","details"}} にする。
// *apperr.Error でないものは internal（原因は応答に出さない）。
// details は文字列（短いもの）・数値・真偽値だけを残し、それ以外の値は落とす。
func ErrorText(err error) string {
	e := apperr.As(err)
	b, merr := json.Marshal(errorBody{Error: errorObject{
		Code: string(e.Code), Message: e.Message(), Details: sanitizeDetails(e.Details),
	}})
	if merr != nil {
		return `{"error":{"code":"internal","message":"` + apperr.Message(apperr.Internal) + `"}}`
	}
	return string(b)
}

// ErrorCode は err のエラーコード（監査ログ用）。
func ErrorCode(err error) string { return string(apperr.As(err).Code) }

func sanitizeDetails(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.String:
			if s := rv.String(); utf8.RuneCountInString(s) <= maxDetailRunes {
				out[k] = s
			}
		case reflect.Bool:
			out[k] = rv.Bool()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out[k] = rv.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			out[k] = rv.Uint()
		case reflect.Float32, reflect.Float64:
			out[k] = rv.Float()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
