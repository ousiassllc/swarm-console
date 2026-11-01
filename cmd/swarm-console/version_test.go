package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/updatecheck/updatechecktest"
)

// useVersion は version と updateEnv をテスト中だけ差し替える（実際の GitHub・実際の ~/bin に触れない）。
func useVersion(t *testing.T, v string, env app.UpdateEnv) {
	t.Helper()
	oldV, oldE := version, updateEnv
	version, updateEnv = v, env
	t.Cleanup(func() { version, updateEnv = oldV, oldE })
}

func releaseJSON(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintf(w, `{"tag_name":%q}`, tag) }
}

func respond(code int, hdr ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(hdr); i += 2 {
			w.Header().Set(hdr[i], hdr[i+1])
		}
		w.WriteHeader(code)
	}
}

// version --check の 2 行目と終了コード（FR-UPD-6・§12.2）。
func TestVersionCheck(t *testing.T) {
	const url = "https://github.com/ousiassllc/swarm-console/releases/tag/v0.1.5"
	tests := []struct {
		name, current string
		h             http.HandlerFunc
		wantLine      string
		wantCode      int
	}{
		{"最新", "v0.1.5", releaseJSON("v0.1.5"), "最新です（最新版 v0.1.5）", 0},
		{"手元のほうが新しい", "v0.2.0", releaseJSON("v0.1.5"), "最新です（最新版 v0.1.5）", 0},
		{"新しい版あり", "v0.1.4", releaseJSON("v0.1.5"), "新しい版があります: v0.1.5（" + url + "）", 10},
		{"network", "v0.1.4", respond(502), "確認できませんでした: GitHub に接続できません", 1},
		{"rate_limit", "v0.1.4", respond(429), "確認できませんでした: GitHub の取得回数の上限に達しました。しばらくしてからやり直してください", 1},
		{"access", "v0.1.4", respond(403), "確認できませんでした: GitHub が取得を拒否しました", 1},
		{"no_release", "v0.1.4", respond(404), "確認できませんでした: 公開されたリリースがありません", 1},
		{"release_invalid", "v0.1.4", releaseJSON("latest"), "確認できませんでした: 最新リリースの版の形が不正です", 1},
		{"dev_build", "dev", releaseJSON("v0.1.5"), "確認しません: この実行ファイルにはリリースの版が入っていません（dev）", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := updatechecktest.New(t)
			o.HandleFunc("api.github.com", tt.h)
			useVersion(t, tt.current, app.UpdateEnv{Transport: o.Transport()})
			code, stdout, stderr := runCLI(t, "version", "--check")
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, want %d (stderr=%q)", code, tt.wantCode, stderr)
			}
			if want := tt.current + "\n" + tt.wantLine + "\n"; stdout != want {
				t.Errorf("標準出力 =\n%q\nwant\n%q", stdout, want)
			}
			if stderr != "" {
				t.Errorf("標準エラー = %q", stderr)
			}
		})
	}
}

// 引数なしの version は今までどおり版を 1 行出すだけで、通信しない。
func TestPlainVersionDoesNotConnect(t *testing.T) {
	o := updatechecktest.New(t)
	o.HandleFunc("api.github.com", releaseJSON("v9.9.9"))
	useVersion(t, "v0.1.4", app.UpdateEnv{Transport: o.Transport()})
	code, stdout, _ := runCLI(t, "version")
	if code != 0 || stdout != "v0.1.4\n" {
		t.Fatalf("exit %d %q", code, stdout)
	}
	if n := len(o.Requests()); n != 0 {
		t.Errorf("通信しました（%d 件）", n)
	}
}

// --check は設定を読まず（壊れた config.toml があっても、update_check = false でも動く）、
// 新しい版があっても自己更新の置き場に触れず、ダウンロードもしない。
func TestVersionCheckReadsNoConfigAndNeverReplacesBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confDir := filepath.Join(home, ".config", "swarm-console")
	binDir := filepath.Join(home, "bin", "swarm-console")
	for _, d := range []string{confDir, binDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.toml"), []byte("not toml = = \nupdate_check = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(binDir, "swarm-console")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil { //nolint:gosec // 実行ファイルを模した一時ファイル
		t.Fatal(err)
	}
	o := updatechecktest.New(t)
	o.HandleFunc("api.github.com", releaseJSON("v0.1.5"))
	o.HandleFunc("github.com", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("NEW")) })
	useVersion(t, "v0.1.4", app.UpdateEnv{Transport: o.Transport(), Home: home, Executable: func() (string, error) { return exe, nil }})
	if code, _, stderr := runCLI(t, "version", "--check"); code != 10 {
		t.Fatalf("exit %d %s", code, stderr)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" { //nolint:gosec // テストが作った一時ファイル
		t.Errorf("バイナリが置き換わりました: %q", b)
	}
	if ents, _ := os.ReadDir(binDir); len(ents) != 1 {
		t.Errorf("置き場にファイルが増えました: %v", ents)
	}
	for _, r := range o.Requests() {
		if r.Host != "api.github.com" {
			t.Errorf("確認以外の通信: %s%s", r.Host, r.Path)
		}
	}
	if n := len(o.Requests()); n != 1 {
		t.Errorf("通信 %d 件, want 1", n)
	}
}

func TestVersionArgs(t *testing.T) {
	if code, _, _ := runCLI(t, "version", "extra"); code != 2 {
		t.Errorf("位置引数つきの version = %d, want 2", code)
	}
	if code, _, stderr := runCLI(t, "version", "--nope"); code != 1 || !strings.Contains(stderr, "nope") {
		t.Errorf("未知のフラグの version = %d %q", code, stderr)
	}
	if !strings.Contains(usage, "version [--check]") {
		t.Error("使い方に version --check がありません")
	}
}
