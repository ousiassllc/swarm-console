package notes

import (
	"encoding/json"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// Check は Set と同じ検証だけを行い、何も書かない（dispatch が送る前に使う）。
func TestCheckMatchesSetWithoutWriting(t *testing.T) {
	e := newEnv(t)
	secret, _ := json.Marshal(dummySecret())
	cases := []struct {
		name string
		set  map[string]json.RawMessage
		code apperr.Code // "" は成功
	}{
		{"正常", map[string]json.RawMessage{"from": json.RawMessage(`"agent-b"`), "items": json.RawMessage(`["#1"]`)}, ""},
		{"null で消す", map[string]json.RawMessage{"next": json.RawMessage(`null`)}, ""},
		{"未知キー", map[string]json.RawMessage{"nope": json.RawMessage(`"x"`)}, apperr.InvalidArgs},
		{"読み取り専用", map[string]json.RawMessage{"log": json.RawMessage(`[]`)}, apperr.InvalidArgs},
		{"型違い", map[string]json.RawMessage{"items": json.RawMessage(`"x"`)}, apperr.InvalidArgs},
		{"秘密", map[string]json.RawMessage{"memo": secret}, apperr.SecretDetected},
	}
	for _, c := range cases {
		err := Check(c.set, testDetector)
		if c.code == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		if got := apperr.As(err).Code; got != c.code {
			t.Errorf("%s: code = %s, want %s", c.name, got, c.code)
		}
		if _, setErr := e.s.Set(t.Context(), "%1", c.set); apperr.As(setErr).Code != c.code {
			t.Errorf("%s: Set と Check のコードが違います: %v", c.name, setErr)
		}
	}
	if ids := e.s.ActiveIDs(); len(ids) != 0 {
		t.Errorf("Check がメモを書いています: %v", ids)
	}
	if err := Check(map[string]json.RawMessage{"memo": secret}, nil); err != nil {
		t.Errorf("detect が nil なら秘密検査は省くはずです: %v", err)
	}
}
