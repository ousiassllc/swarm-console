package tooltests_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

func TestErrorText(t *testing.T) {
	err := apperr.New(apperr.InvalidState, "pane", "B4", "state", "working", "body", strings.Repeat("x", 101),
		"nested", map[string]any{"a": 1}, "n", 3)
	text := toolkit.ErrorText(err)
	want := `{"error":{"code":"invalid_state","message":"このペインの状態では操作できません","details":{"n":3,"pane":"B4","state":"working"}}}`
	if text != want {
		t.Errorf("ErrorText =\n%s\nwant\n%s", text, want)
	}
	var back struct {
		Error struct{ Code, Message string }
	}
	if json.Unmarshal([]byte(text), &back) != nil || back.Error.Code != "invalid_state" {
		t.Errorf("JSON として読めません: %s", text)
	}
}

// *apperr.Error でないエラーは internal に包み、原因の文言を応答に出さない。details が無ければ省く。
func TestErrorTextHidesCause(t *testing.T) {
	text := toolkit.ErrorText(errors.New("dial tcp: secret-token-abc"))
	if strings.Contains(text, "secret-token-abc") || !strings.Contains(text, `"code":"internal"`) || strings.Contains(text, "details") {
		t.Errorf("ErrorText = %s", text)
	}
	if toolkit.ErrorCode(apperr.Wrap(apperr.TmuxError, errors.New("x"))) != "tmux_error" {
		t.Error("ErrorCode が違います")
	}
}
