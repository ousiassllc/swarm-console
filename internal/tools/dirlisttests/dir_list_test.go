package dirlisttests_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
)

// path を省略すると allowed_roots そのものを返す（path は出さない）。
func TestDirListOmittedPathReturnsAllowedRoots(t *testing.T) {
	second := realTempDir(t)
	e := newEnv(t, func(_ string, cfg *config.Config) { cfg.AllowedRoots = append(cfg.AllowedRoots, second) })
	for _, args := range []string{``, `{}`, `null`} {
		l, err := dirList(t, e.Fixture, args)
		if err != nil {
			t.Fatalf("args=%q: %v", args, err)
		}
		var got []string
		for _, en := range l.Entries {
			got = append(got, en.Path)
		}
		slices.Sort(got)
		want := []string{e.Root, second}
		slices.Sort(want)
		if !slices.Equal(got, want) || l.Truncated || l.Path != "" {
			t.Errorf("args=%q: entries=%v truncated=%v path=%q, want roots %v", args, got, l.Truncated, l.Path, want)
		}
	}
}

// 存在しない・denied_paths の下にある allowed_roots は、pane_start に渡せないので載せない。
func TestDirListOmittedPathSkipsUnusableRoots(t *testing.T) {
	e := newEnv(t, func(root string, cfg *config.Config) {
		cfg.AllowedRoots = append(cfg.AllowedRoots, filepath.Join(root, "missing"), filepath.Join(root, "denied"))
		cfg.DeniedPaths = []string{filepath.Join(root, "denied")}
	})
	mkdir(t, filepath.Join(e.Root, "denied"))
	l, err := dirList(t, e.Fixture, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 || l.Entries[0].Path != e.Root {
		t.Errorf("entries = %+v, want root %s だけ", l.Entries, e.Root)
	}
}

// 根そのもの・根の直下・その下の階層。返るのは直下のサブディレクトリだけで、名前順。
func TestDirListBoundaries(t *testing.T) {
	e := newEnv(t, nil)
	if err := os.Remove(e.Cfg.ReportDir); err != nil { // Fixture が根の直下に作る reports を外す
		t.Fatal(err)
	}
	for _, d := range []string{"b-dir", "a-dir", "c-dir/nested"} {
		mkdir(t, filepath.Join(e.Root, d))
	}
	writeFile(t, filepath.Join(e.Root, "file.txt"))
	mkdir(t, filepath.Join(e.Root, ".hidden"))
	mkdir(t, filepath.Join(e.Root, ".git"))

	tests := []struct {
		name string
		path string
		want []string
	}{
		{"root_itself", e.Root, []string{"a-dir", "b-dir", "c-dir"}},
		{"root_with_trailing_slash", e.Root + "/", []string{"a-dir", "b-dir", "c-dir"}},
		{"root_child", filepath.Join(e.Root, "c-dir"), []string{"nested"}},
		{"root_child_without_subdirs", filepath.Join(e.Root, "a-dir"), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := dirList(t, e.Fixture, `{"path":`+quote(tt.path)+`}`)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(l); !slices.Equal(got, tt.want) || l.Truncated {
				t.Errorf("entries = %v truncated=%v, want %v", got, l.Truncated, tt.want)
			}
			if l.Path != filepath.Clean(tt.path) {
				t.Errorf("path = %q, want %q", l.Path, filepath.Clean(tt.path))
			}
			for _, en := range l.Entries {
				if en.Path != filepath.Join(l.Path, en.Name) {
					t.Errorf("entry %+v の path が <path>/<name> ではありません", en)
				}
			}
		})
	}
}

// 空のディレクトリは entries が null ではなく []。
func TestDirListEmptyEntriesIsArray(t *testing.T) {
	e := newEnv(t, nil)
	text, err := call(t, e.Fixture, "dir_list", `{"path":`+quote(mkdir(t, filepath.Join(e.Root, "empty")))+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"entries":[]`) {
		t.Errorf("本文 = %s", text)
	}
}

// ファイル・隠しディレクトリ・denied_paths の配下・許可の外へ出るリンク・秘密の名前は載せない。許可の中を指すリンクは載せる。
func TestDirListExcludesUnusableEntries(t *testing.T) {
	e := newEnv(t, func(root string, cfg *config.Config) {
		cfg.DeniedPaths = []string{filepath.Join(root, "work", "denied")}
	})
	work := filepath.Join(e.Root, "work")
	mkdir(t, filepath.Join(work, "visible"))
	mkdir(t, filepath.Join(work, "denied", "inner"))
	mkdir(t, filepath.Join(work, "inside-target"))
	mkdir(t, filepath.Join(work, ".hidden"))
	mkdir(t, filepath.Join(work, "id_rsa")) // file_get が名前で断る形（秘密）
	mkdir(t, filepath.Join(work, "with'quote"))
	writeFile(t, filepath.Join(work, "plain.txt"))
	symlink(t, filepath.Join(work, "inside-target"), filepath.Join(work, "link-inside"))
	symlink(t, e.outside, filepath.Join(work, "link-escaping"))
	symlink(t, filepath.Join(work, "denied"), filepath.Join(work, "link-to-denied"))
	symlink(t, filepath.Join(work, "plain.txt"), filepath.Join(work, "link-to-file"))
	symlink(t, filepath.Join(work, "no-such"), filepath.Join(work, "link-dangling"))

	l, err := dirList(t, e.Fixture, `{"path":`+quote(work)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"inside-target", "link-inside", "visible"}
	if got := names(l); !slices.Equal(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
}

// git は直下に .git（ディレクトリ・worktree のファイル）があるときだけ true。
func TestDirListGitFlag(t *testing.T) {
	e := newEnv(t, nil)
	if err := os.Remove(e.Cfg.ReportDir); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(e.Root, "repo", ".git"))
	writeFile(t, filepath.Join(e.Root, "worktree", ".git")) // git worktree の .git はファイル
	mkdir(t, filepath.Join(e.Root, "plain"))
	mkdir(t, filepath.Join(e.Root, "outer", "inner", ".git")) // 孫の .git は outer の git ではない
	l, err := dirList(t, e.Fixture, `{"path":`+quote(e.Root)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, en := range l.Entries {
		got[en.Name] = en.Git
	}
	want := map[string]bool{"repo": true, "worktree": true, "plain": false, "outer": false}
	if len(got) != len(want) {
		t.Fatalf("entries = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: git = %v, want %v", k, got[k], v)
		}
	}
}

// limit: 既定 200・上限 500・超えたら truncated。ちょうどの件数は truncated にしない。
func TestDirListLimitAndTruncated(t *testing.T) {
	e := newEnv(t, nil)
	if err := os.Remove(e.Cfg.ReportDir); err != nil {
		t.Fatal(err)
	}
	for i := range 205 {
		mkdir(t, filepath.Join(e.Root, fmt.Sprintf("d%03d", i)))
	}
	tests := []struct {
		name          string
		args          string
		wantN         int
		wantTruncated bool
	}{
		{"default_200", `{"path":` + quote(e.Root) + `}`, 200, true},
		{"limit_1", `{"path":` + quote(e.Root) + `,"limit":1}`, 1, true},
		{"limit_equals_count", `{"path":` + quote(e.Root) + `,"limit":205}`, 205, false},
		{"limit_500", `{"path":` + quote(e.Root) + `,"limit":500}`, 205, false},
		{"limit_below_count", `{"path":` + quote(e.Root) + `,"limit":204}`, 204, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := dirList(t, e.Fixture, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if len(l.Entries) != tt.wantN || l.Truncated != tt.wantTruncated {
				t.Errorf("件数 %d truncated=%v, want %d / %v", len(l.Entries), l.Truncated, tt.wantN, tt.wantTruncated)
			}
			if l.Entries[0].Name != "d000" { // 名前順の先頭から返す
				t.Errorf("先頭 = %s", l.Entries[0].Name)
			}
		})
	}
}
