package notes

import (
	"encoding/json"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// Check は Set と同じ検証（型・上限・秘密検査）だけを行い、何も書かない。
// dispatch が「何かを送る前に」メモを検証するために使う。detect が nil なら秘密検査は省く。
// 検証で落ちるのは Set と同じ *apperr.Error（invalid_args・secret_detected）。
func Check(set map[string]json.RawMessage, detect SecretDetector) error {
	changes, err := parseChanges(set)
	if err != nil {
		return err
	}
	if detect == nil {
		return nil
	}
	for _, c := range changes {
		for _, str := range c.strs {
			if str != "" && detect(str) {
				return apperr.New(apperr.SecretDetected, "field", c.key)
			}
		}
	}
	return nil
}
