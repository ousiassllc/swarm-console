// Package startoptstests は pane_start・dispatch・note_set の起動時の model・thinking（Issue #124）のツール層のテスト。
// 道具（call・mustCall・wantCode）は tooltests の複製（tooltests はディレクトリの行数の上限のため、ここへ分けた）。
package startoptstests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

const dispatchOK = `"agent":"omp","task":"#982 を直す","profile":"fix","command":"/issue-sweep #982"`

var testClient = auth.Client{Name: "tester", Scope: auth.ScopeOps}

// call は /ops に出ているツール name を args で呼ぶ。
func call(t *testing.T, f *toolstest.Fixture, name, args string) (string, error) {
	t.Helper()
	tool, ok := f.Tools.Lookup(toolkit.EndpointOps, name)
	if !ok {
		t.Fatalf("ツール %s がありません", name)
	}
	res, err := tool.Handler(context.Background(), testClient, json.RawMessage(args))
	return res.Text, err
}

// mustCall は成功を期待して JSON を map にして返す。
func mustCall(t *testing.T, f *toolstest.Fixture, name, args string) map[string]any {
	t.Helper()
	text, err := call(t, f, name, args)
	if err != nil {
		t.Fatalf("%s(%s): %v", name, args, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%s の返り値が JSON ではありません: %q", name, text)
	}
	return m
}

// wantCode は err が code の *apperr.Error であることを確かめ、details を返す。
func wantCode(t *testing.T, err error, code apperr.Code) map[string]any {
	t.Helper()
	if err == nil {
		t.Fatalf("エラー %s を期待しましたが成功しました", code)
	}
	e := apperr.As(err)
	if e.Code != code {
		t.Fatalf("エラーコード = %s, want %s (%v)", e.Code, code, err)
	}
	return e.Details
}
