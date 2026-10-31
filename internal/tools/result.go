package tools

import (
	"bytes"
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// marshalResult は v を HTML エスケープなしの 1 行 JSON にして Result にする（キー順は構造体の宣言順）。
func marshalResult(v any) (Result, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return Result{}, apperr.Wrap(apperr.Internal, err)
	}
	return Result{Text: string(bytes.TrimRight(buf.Bytes(), "\n"))}, nil
}
