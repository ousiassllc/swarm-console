package selfupdatetests

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/selfupdate"
)

var errInjected = errors.New("injected")

// failFS は指定した操作だけを失敗させる fsOps（各ステップの失敗で本体が元のまま残ることの確認）。
type failFS struct {
	selfupdate.OSFS
	fail string // remove-new・remove-old・link・marker・rename・create・write・sync
}

func (f failFS) Remove(path string) error {
	switch {
	case f.fail == "remove-new" && strings.HasSuffix(path, newName):
		return errInjected
	case f.fail == "remove-old" && strings.HasSuffix(path, oldName):
		return errInjected
	}
	return f.OSFS.Remove(path)
}

func (f failFS) Link(oldname, newname string) error {
	if f.fail == "link" {
		return errInjected
	}
	return f.OSFS.Link(oldname, newname)
}

func (f failFS) Rename(oldpath, newpath string) error {
	if f.fail == "rename" {
		return errInjected
	}
	return f.OSFS.Rename(oldpath, newpath)
}

func (f failFS) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if f.fail == "marker" {
		return errInjected
	}
	return f.OSFS.WriteFileAtomic(path, data, perm)
}

func (f failFS) Create(path string, perm os.FileMode) (selfupdate.StepFile, error) {
	if f.fail == "create" {
		return nil, errInjected
	}
	file, err := f.OSFS.Create(path, perm)
	if err != nil {
		return nil, err
	}
	return failFile{StepFile: file, fail: f.fail}, nil
}

type failFile struct {
	selfupdate.StepFile
	fail string
}

func (f failFile) Write(p []byte) (int, error) {
	if f.fail == "write" {
		return 0, errInjected
	}
	return f.StepFile.Write(p)
}

func (f failFile) Sync() error {
	if f.fail == "sync" {
		return errInjected
	}
	return f.StepFile.Sync()
}

// 各ステップ（.new の書き込み・.old のリンク・マーカー・rename）の失敗で、本体は旧バイナリのまま、.new とマーカーは消える。
func TestApplyStepFailuresLeaveOriginal(t *testing.T) {
	for _, step := range []string{"remove-new", "create", "write", "sync", "remove-old", "link", "marker", "rename"} {
		t.Run(step, func(t *testing.T) {
			g := newRig(t, func(o *Options) { o.FS = failFS{fail: step} })
			g.serve(okRelease(newVersion, newContent))
			wantFailure(t, g.apply(newVersion), selfupdate.ReasonWrite)
			if got := g.read(binName); got != oldContent {
				t.Errorf("本体が変わっています: %q", got)
			}
			for _, name := range []string{newName, pendingName} {
				if g.exists(name) {
					t.Errorf("%s が残っています", name)
				}
			}
			// 一時ファイル（マーカーの .tmp-*）も残らない。.old は旧バイナリのハードリンクで、残っても害はない。
			for _, name := range g.files() {
				if name != binName && name != oldName {
					t.Errorf("想定外のファイル %s", name)
				}
			}
			if g.exits.Load() != 0 {
				t.Error("失敗したのに終了を要求しました")
			}
			g.wantApply(selfupdate.StateFailed, selfupdate.ReasonWrite)
		})
	}
}

// rename に失敗したとき、すでに置いたマーカーは消す。前の試運転のマーカーは、マーカー書き込みの失敗では消さない。
func TestApplyMarkerFailureKeepsPreviousMarker(t *testing.T) {
	g := newRig(t, func(o *Options) { o.FS = failFS{fail: "marker"} })
	prev := `{"from":"v0.1.2","to":"v0.1.4","boots":1}`
	if err := os.WriteFile(filepath.Join(g.dir, pendingName), []byte(prev), 0o600); err != nil {
		t.Fatal(err)
	}
	g.serve(okRelease(newVersion, newContent))
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonWrite)
	if got := g.read(pendingName); got != prev {
		t.Errorf("前のマーカーが変わりました: %q", got)
	}
}

// 実ファイルでの失敗：置き場の .old が空でないディレクトリで消せない → write で、本体は元のまま。
func TestApplyRealFilesystemFailure(t *testing.T) {
	g := newRig(t)
	g.serve(okRelease(newVersion, newContent))
	oldDir := filepath.Join(g.dir, oldName)
	if err := os.MkdirAll(filepath.Join(oldDir, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonWrite)
	if g.read(binName) != oldContent || g.exists(newName) || g.exists(pendingName) {
		t.Errorf("置き場 = %v", g.files())
	}
}

// 再試行で、次の周期に成功すれば failed は消える（成功は終了するので、ここでは Apply が通ることだけ）。
func TestApplyRetryAfterFailure(t *testing.T) {
	g := newRig(t)
	g.serve(release{tag: newVersion, sums: code(500), binary: body(newContent)})
	wantFailure(t, g.apply(newVersion), selfupdate.ReasonDownload)
	g.serve(okRelease(newVersion, newContent))
	if err := g.apply(newVersion); err != nil {
		t.Fatal(err)
	}
	if g.read(binName) != newContent || g.exits.Load() != 1 {
		t.Error("再試行で入れ替わっていません")
	}
}

// 新バイナリを実行せず systemctl も実行しない（NFR-SEC-16）：パッケージが os/exec・syscall.Exec を使わないこと。
func TestPackageDoesNotExecute(t *testing.T) {
	files, err := filepath.Glob("../*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); slices.Contains([]string{"os/exec", "golang.org/x/sys/unix"}, p) {
				t.Errorf("%s が %s を import しています（コマンド・新バイナリを実行しない）", f, p)
			}
		}
		if b, _ := os.ReadFile(f); strings.Contains(string(b), "syscall.Exec") || strings.Contains(string(b), "StartProcess") { //nolint:gosec // 自パッケージの固定の相対パス
			t.Errorf("%s がプロセスを起動しています", f)
		}
	}
	if checked < 5 {
		t.Fatalf("検査したファイルが %d 個しかありません", checked)
	}
}
