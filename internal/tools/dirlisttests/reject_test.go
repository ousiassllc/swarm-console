package dirlisttests_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

// path_forbidden の reason は file_get と同じ語彙（not_absolute・dotdot・outside_roots・denied・secret）。
// 存在しない外のパスも outside_roots で、存在を漏らさない。
func TestDirListRejectsPaths(t *testing.T) {
	e := newEnv(t, func(root string, cfg *config.Config) { cfg.DeniedPaths = []string{filepath.Join(root, "denied")} })
	mkdir(t, filepath.Join(e.Root, "denied", "sub"))
	mkdir(t, filepath.Join(e.Root, "keys", "id_rsa"))
	mkdir(t, filepath.Join(e.Root, "proj", "secrets.pem"))
	mkdir(t, filepath.Join(e.Root, "work"))
	symlink(t, e.outside, filepath.Join(e.Root, "work", "escape"))
	symlink(t, filepath.Join(e.Root, "denied"), filepath.Join(e.Root, "work", "to-denied"))
	writeFile(t, filepath.Join(e.Root, "work", "plain.txt"))

	tests := []struct {
		name   string
		path   string
		code   apperr.Code
		reason string
	}{
		{"dotdot", e.Root + "/work/../denied", apperr.PathForbidden, "dotdot"},
		{"dotdot_escaping_root", e.Root + "/..", apperr.PathForbidden, "dotdot"},
		{"relative_path", "work/sub", apperr.PathForbidden, "not_absolute"},
		{"relative_dot", ".", apperr.PathForbidden, "not_absolute"},
		{"outside_roots_existing", e.outside, apperr.PathForbidden, "outside_roots"},
		{"outside_roots_nonexistent", filepath.Join(e.outside, "no", "such"), apperr.PathForbidden, "outside_roots"},
		{"outside_roots_sibling_prefix", e.Root + "-sibling", apperr.PathForbidden, "outside_roots"},
		{"denied_exact", filepath.Join(e.Root, "denied"), apperr.PathForbidden, "denied"},
		{"denied_subtree", filepath.Join(e.Root, "denied", "sub"), apperr.PathForbidden, "denied"},
		{"denied_via_symlink", filepath.Join(e.Root, "work", "to-denied"), apperr.PathForbidden, "denied"},
		{"secret_dir_name_id_rsa", filepath.Join(e.Root, "keys", "id_rsa"), apperr.PathForbidden, "secret"},
		{"secret_dir_name_pem", filepath.Join(e.Root, "proj", "secrets.pem"), apperr.PathForbidden, "secret"},
		{"symlink_escaping_roots", filepath.Join(e.Root, "work", "escape"), apperr.PathForbidden, "outside_roots"},
		{"not_found_under_root", filepath.Join(e.Root, "no-such-dir"), apperr.NotFound, ""},
		{"not_a_directory", filepath.Join(e.Root, "work", "plain.txt"), apperr.PathForbidden, "not_directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dirList(t, e.Fixture, `{"path":`+quote(tt.path)+`}`)
			d := wantCode(t, err, tt.code)
			if tt.reason != "" {
				wantReason(t, d, tt.reason)
			}
			if d["field"] != "path" {
				t.Errorf("details.field = %v, want path", d["field"])
			}
		})
	}
}

// 引数の誤りは invalid_args（details.field に引数名）。他のツールと同じ形。
func TestDirListInvalidArgs(t *testing.T) {
	e := newEnv(t, nil)
	tests := []struct{ name, args, field string }{
		{"limit_zero", `{"limit":0}`, "limit"},
		{"limit_negative", `{"limit":-1}`, "limit"},
		{"limit_501", `{"limit":501}`, "limit"},
		{"limit_string", `{"limit":"10"}`, "limit"},
		{"limit_float", `{"limit":1.5}`, "limit"},
		{"path_empty", `{"path":""}`, "path"},
		{"path_number", `{"path":1}`, "path"},
		{"path_control_char", `{"path":"` + "/tmp/a\\nb" + `"}`, "path"},
		{"unknown_key", `{"dir":"/"}`, "dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dirList(t, e.Fixture, tt.args)
			if d := wantCode(t, err, apperr.InvalidArgs); d["field"] != tt.field {
				t.Errorf("details = %v, want field=%s", d, tt.field)
			}
		})
	}
}

// dir_list は /ops だけ。/report の tools/list に出ず、Lookup できない（呼ぶと mcpapi が unknown_tool にする）。
// 個数は /ops 20・/report 10。
func TestDirListIsOpsOnly(t *testing.T) {
	e := newEnv(t, nil)
	for endpoint, want := range map[string]struct {
		count  int
		listed bool
	}{toolkit.EndpointReport: {10, false}, toolkit.EndpointOps: {20, true}} {
		var found bool
		ts := e.Tools.ForEndpoint(endpoint)
		for _, tl := range ts {
			found = found || tl.Name == "dir_list"
		}
		if len(ts) != want.count || found != want.listed {
			t.Errorf("%s: %d ツール, dir_list=%v, want %d / %v", endpoint, len(ts), found, want.count, want.listed)
		}
		if _, ok := e.Tools.Lookup(endpoint, "dir_list"); ok != want.listed {
			t.Errorf("%s: Lookup(dir_list) = %v, want %v", endpoint, ok, want.listed)
		}
	}
}

// dir_list に出たパスは、pane_start の cwd で forbidden_path にならない（判定の共通化）。
// 根・根の直下・許可の中を指すリンクを含めて、出たもの全部を pane_start に渡す。
func TestDirListResultsAreAcceptedByPaneStart(t *testing.T) {
	e := newEnv(t, func(root string, cfg *config.Config) { cfg.DeniedPaths = []string{filepath.Join(root, "denied")} })
	mkdir(t, filepath.Join(e.Root, "a"))
	mkdir(t, filepath.Join(e.Root, "b", "c"))
	mkdir(t, filepath.Join(e.Root, "denied", "x"))
	mkdir(t, filepath.Join(e.Root, "space dir"))
	mkdir(t, filepath.Join(e.Root, "it's"))
	symlink(t, filepath.Join(e.Root, "b"), filepath.Join(e.Root, "link-b"))
	symlink(t, e.outside, filepath.Join(e.Root, "link-out"))

	var paths []string
	for _, args := range []string{`{}`, `{"path":` + quote(e.Root) + `}`, `{"path":` + quote(filepath.Join(e.Root, "b")) + `}`} {
		l, err := dirList(t, e.Fixture, args)
		if err != nil {
			t.Fatal(err)
		}
		for _, en := range l.Entries {
			paths = append(paths, en.Path)
		}
	}
	if len(paths) < 5 {
		t.Fatalf("テストの前提が崩れています: %v", paths)
	}
	for _, p := range paths {
		if _, err := call(t, e.Fixture, "pane_start", `{"pane":"B1","agent":"omp","title":"t","cwd":`+quote(p)+`}`); err != nil {
			t.Errorf("dir_list が返した %s を pane_start が断りました: %v", p, err)
		}
		if _, err := dirList(t, e.Fixture, `{"path":`+quote(p)+`}`); err != nil {
			t.Errorf("dir_list が返した %s をさらに dir_list できません: %v", p, err)
		}
	}
	// 載せなかったものは、pane_start も断る（denied・外へ出るリンク）。「'」の名前は pane_start が invalid_args にするので載せていない。
	for _, p := range []string{filepath.Join(e.Root, "denied"), filepath.Join(e.Root, "link-out")} {
		if slices.Contains(paths, p) {
			t.Errorf("%s が載っています", p)
		}
		_, err := call(t, e.Fixture, "pane_start", `{"pane":"B1","agent":"omp","title":"t","cwd":`+quote(p)+`}`)
		wantCode(t, err, apperr.ForbiddenPath)
	}
	if slices.Contains(paths, filepath.Join(e.Root, "it's")) {
		t.Error("「'」を含む名前が載っています（pane_start の cwd にできない）")
	}
}
