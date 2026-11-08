// Package dirlisttests は dir_list（/ops の読み取りツール。pane_start の cwd の候補を 1 階層ずつ返す）のテスト。
package dirlisttests_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/safety"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
	"github.com/ousiassllc/swarm-console/internal/tools/toolstest"
)

// env は allowed_roots が root 1 つの Fixture。outside は allowed_roots の外の別ディレクトリ。
type env struct {
	*toolstest.Fixture
	outside string
}

// newEnv は Fixture を作り、mutate で設定（denied_paths など）を変えてから、Policy とツールを作りなおす。
func newEnv(t *testing.T, mutate func(root string, cfg *config.Config)) *env {
	t.Helper()
	f := toolstest.New(t)
	if mutate != nil {
		mutate(f.Root, f.Cfg)
	}
	f.Deps.Safety = safety.NewPolicy(f.Cfg)
	var err error
	if f.Tools, err = tools.New(f.Deps); err != nil {
		t.Fatal(err)
	}
	return &env{Fixture: f, outside: realTempDir(t)}
}

func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path string) string {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func symlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}

var opsClient = auth.Client{Name: "tester", Scope: auth.ScopeOps}

// call は /ops に出ているツールを args で呼ぶ。
func call(t *testing.T, f *toolstest.Fixture, name, args string) (string, error) {
	t.Helper()
	tool, ok := f.Tools.Lookup(toolkit.EndpointOps, name)
	if !ok {
		t.Fatalf("ツール %s が /ops にありません", name)
	}
	res, err := tool.Handler(context.Background(), opsClient, json.RawMessage(args))
	return res.Text, err
}

type entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Git  bool   `json:"git"`
}

type listing struct {
	Path      string  `json:"path"`
	Entries   []entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// dirList は dir_list を呼んで結果を読む。args は JSON オブジェクトの本文。
func dirList(t *testing.T, f *toolstest.Fixture, args string) (listing, error) {
	t.Helper()
	text, err := call(t, f, "dir_list", args)
	if err != nil {
		return listing{}, err
	}
	var l listing
	if err := json.Unmarshal([]byte(text), &l); err != nil {
		t.Fatalf("dir_list の返り値が JSON ではありません: %q", text)
	}
	return l, nil
}

func names(l listing) []string {
	out := make([]string, len(l.Entries))
	for i, e := range l.Entries {
		out[i] = e.Name
	}
	return out
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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

func wantReason(t *testing.T, d map[string]any, reason string) {
	t.Helper()
	if d["reason"] != reason {
		t.Errorf("details.reason = %v, want %s", d["reason"], reason)
	}
}
