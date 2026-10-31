package tools

// 引数の JSON Schema を組み立てる小さな部品。どのツールも additionalProperties:false（未知キー拒否）。

type props map[string]any

func object(required []string, p props) map[string]any {
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

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func enumProp(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

func intProp(desc string, minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": minimum, "maximum": maximum}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func strArrayProp(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
}

func objectProp(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc}
}

// panePropDesc は pane 引数の共通説明。
const panePropDesc = "位置（T1・work:B2・w1.T2）または tmux ID（%0）"

func paneProp() map[string]any { return strProp(panePropDesc) }
