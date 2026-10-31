package notes

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/clock"
)

// テスト用の簡易検出器（本番は safety.DetectSecret を渡す）。
var secretRE = regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{16,}|eyJ[A-Za-z0-9_-]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`)

func testDetector(s string) bool { return secretRE.MatchString(s) }

// dummySecret は検出器に当たるダミー文字列（実在の鍵ではない）。
func dummySecret() string { return "ghp_" + strings.Repeat("x", 30) }

var t0 = time.Date(2026, 10, 31, 22, 10, 5, 0, time.FixedZone("JST", 9*3600))

type env struct {
	t    *testing.T
	path string
	clk  *clock.Fake
	s    Store
}

func newEnv(t *testing.T, opts ...Option) *env {
	t.Helper()
	e := &env{t: t, path: filepath.Join(t.TempDir(), "data", "notes.json"), clk: clock.NewFake(t0)}
	e.s = e.open(opts...)
	return e
}

func (e *env) open(opts ...Option) Store {
	e.t.Helper()
	opts = append([]Option{WithSecretDetector(testDetector)}, opts...)
	s, err := NewStore(e.path, e.clk, opts...)
	if err != nil {
		e.t.Fatalf("NewStore: %v", err)
	}
	return s
}

func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func setOf(kv map[string]any) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(kv))
	for k, v := range kv {
		out[k] = mustRaw(v)
	}
	return out
}

func (e *env) set(id string, kv map[string]any) *Note {
	e.t.Helper()
	n, err := e.s.Set(context.Background(), id, setOf(kv))
	if err != nil {
		e.t.Fatalf("Set: %v", err)
	}
	return n
}

// wantCode は err が code の *apperr.Error で、details.field が field であることを確かめる。
func wantCode(t *testing.T, err error, code apperr.Code, field string) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("*apperr.Error ではない: %v", err)
	}
	if ae.Code != code {
		t.Fatalf("code = %s, want %s", ae.Code, code)
	}
	if field != "" && ae.Details["field"] != field {
		t.Fatalf("details.field = %v, want %s", ae.Details["field"], field)
	}
}
