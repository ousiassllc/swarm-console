package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/test/integration/harness"
)

// logLines は buf の JSON Lines のうち msg が want の行を返す。JSON でない行があれば失敗にする。
func logLines(t *testing.T, buf *bytes.Buffer, want string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("JSON でないログ行: %q: %v", line, err)
		}
		if rec["msg"] == want {
			out = append(out, rec)
		}
	}
	return out
}

// NFR-OPS-5: auth と notes の警告も、app.New に渡したロガー（JSON Lines）へ出る。
func TestAuthAndNotesWarningsUseInjectedLogger(t *testing.T) {
	h := harness.Prepare(t, harness.Options{NoTmuxSession: true})
	notesPath := filepath.Join(h.Dir, ".local", "share", "swarm-console", "notes.json")
	h.WriteFile(notesPath, "{壊れた")
	var buf bytes.Buffer
	a, err := app.New(h.Config, "test", slog.New(slog.NewJSONHandler(&buf, nil)))
	if err != nil {
		t.Fatal(err)
	}

	const notesMsg = "notes.json が壊れているため退避して空で開始します"
	recs := logLines(t, &buf, notesMsg)
	if len(recs) != 1 {
		t.Fatalf("notes の警告が %d 行（want 1）: %s", len(recs), buf.String())
	}
	if recs[0]["level"] != "WARN" || recs[0]["path"] != notesPath || recs[0]["moved_to"] == nil || recs[0]["reason"] == nil {
		t.Errorf("notes の警告の属性が違う: %v", recs[0])
	}

	// 権限を 644 にした tokens.toml は、次の認証で読み込めず警告になる。
	//nolint:gosec // 緩い権限の tokens.toml を読ませる検証
	if err := os.Chmod(h.TokensPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/ops", nil)
	req.Header.Set("Authorization", "Bearer "+h.Token(harness.OpsA))
	rec := httptest.NewRecorder()
	a.Server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d（権限不良の tokens.toml は認証を拒否する）", rec.Code)
	}
	const authMsg = "tokens.toml を読み込めないため認証を拒否します"
	recs = logLines(t, &buf, authMsg)
	if len(recs) != 1 {
		t.Fatalf("auth の警告が %d 行（want 1）: %s", len(recs), buf.String())
	}
	if recs[0]["level"] != "WARN" || recs[0]["error"] == nil {
		t.Errorf("auth の警告の属性が違う: %v", recs[0])
	}
}
