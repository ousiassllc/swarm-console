package toolkit

import "slices"

// 引数の JSON Schema を組み立てる小さな部品。どのツールも additionalProperties:false（未知キー拒否）。

// Props は JSON Schema の properties。
type Props map[string]any

// Object は引数オブジェクトの Schema（required が空なら required キーなし）。
func Object(required []string, p Props) map[string]any {
	s := map[string]any{
		"type":                 "object",
		"properties":           map[string]any(p),
		"additionalProperties": false,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// StrProp は文字列の引数。
func StrProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// EnumProp は values のどれかを取る文字列の引数。
func EnumProp(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": slices.Clone(values)}
}

// IntProp は minimum 以上 maximum 以下の整数の引数。
func IntProp(desc string, minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": minimum, "maximum": maximum}
}

// BoolProp は真偽値の引数。
func BoolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// StrArrayProp は文字列の配列の引数。
func StrArrayProp(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
}

// ObjectProp はオブジェクトの引数。
func ObjectProp(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc}
}

// PanePropDesc は pane 引数の共通説明。
const PanePropDesc = "位置（T1・work:B2・w1.T2）または tmux ID（%0）"

// PaneProp は pane 引数。
func PaneProp() map[string]any { return StrProp(PanePropDesc) }

// PaneArgs は session と pane だけを取るツールの引数。
type PaneArgs struct {
	Session *string `json:"session"`
	Pane    *string `json:"pane"`
}
