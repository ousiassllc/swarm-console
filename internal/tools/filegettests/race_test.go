package filegettests_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/tools"
	"github.com/ousiassllc/swarm-console/internal/tools/toolkit"
)

// withHooks は file_get の差し込み点を付けてツールを作りなおす。
func (e *fileGetEnv) withHooks(t *testing.T, h *toolkit.FileGetHooks) {
	t.Helper()
	e.Deps.FileGet = h
	var err error
	if e.Tools, err = tools.New(e.Deps); err != nil {
		t.Fatal(err)
	}
}

// fileGetAudited は file_get を呼び、ツールが監査へ足した値も返す。
func fileGetAudited(t *testing.T, e *fileGetEnv, path string) (*toolkit.Audit, error) {
	t.Helper()
	tool, ok := e.Tools.Lookup(toolkit.EndpointOps, "file_get")
	if !ok {
		t.Fatal("file_get がありません")
	}
	ctx, au := toolkit.WithAudit(context.Background())
	_, err := tool.Handler(ctx, testClient, json.RawMessage(`{"path":`+quote(path)+`}`))
	return au, err
}

type countingReader struct {
	r io.Reader
	n *int
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	*c.n += n
	return n, err
}

// 確認のあと open の前に、途中のディレクトリが外へのリンクに差し替えられても、外のファイルを返さない
// （開いた fd の実パスの確認 = safety.CheckOpened が効く）。
func TestFileGetRejectsDirectorySwappedBeforeOpen(t *testing.T) {
	e := newFileGetEnv(t)
	dir := filepath.Join(e.work, "dir")
	path := writeFile(t, filepath.Join(dir, "a.png"), pngBytes)
	writeFile(t, filepath.Join(e.outside, "a.png"), []byte("\x89PNG\r\n\x1a\nOUTSIDE"))
	e.withHooks(t, &toolkit.FileGetHooks{BeforeOpen: func(string) {
		if err := os.Rename(dir, filepath.Join(e.work, "moved")); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(e.outside, dir); err != nil {
			t.Error(err)
		}
	}})
	got, err := fileGet(t, e.Fixture, path)
	if err == nil {
		t.Fatalf("差し替えられたのに外のファイルが返りました: %v", got)
	}
	wantReason(t, wantCode(t, err, apperr.PathForbidden), "unreadable")
}

// 確認（stat）のあとに大きくされて上限を超えたら too_large。上限を超えて読まず、実際のサイズは分からないので監査にサイズを残さない。
func TestFileGetTooLargeWhenFileGrowsAfterStat(t *testing.T) {
	e := newFileGetEnv(t)
	e.Cfg.FileGetMaxBytes = 64
	path := writeFile(t, filepath.Join(e.work, "grow.png"), pngBytes)
	read := 0
	e.withHooks(t, &toolkit.FileGetHooks{WrapReader: func(r io.Reader) io.Reader {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // テストが作った一時ファイル
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		if _, err := f.Write(make([]byte, 1<<20)); err != nil {
			t.Fatal(err)
		}
		return countingReader{r, &read}
	}})
	au, err := fileGetAudited(t, e, path)
	d := wantCode(t, err, apperr.TooLarge)
	if d["max"] != int64(64) {
		t.Errorf("details = %v", d)
	}
	if read != 65 {
		t.Errorf("読んだバイト数 = %d, want 上限＋1 の 65", read)
	}
	if au.HasSize {
		t.Errorf("肥大化で上限を超えたのに size を記録しました: %d", au.Size)
	}
}

// 拡張子は画像でも中身が違う大きなファイルは、先頭だけ読んで unsupported_type にする（全部は読まない）。
func TestFileGetSniffsBeforeReadingBody(t *testing.T) {
	e := newFileGetEnv(t)
	e.Cfg.FileGetMaxBytes = 4 << 20
	text := writeFile(t, filepath.Join(e.work, "big.png"), make([]byte, 2<<20))
	good := writeFile(t, filepath.Join(e.work, "good.png"), append(append([]byte{}, pngBytes...), make([]byte, 1<<20)...))
	read := 0
	e.withHooks(t, &toolkit.FileGetHooks{WrapReader: func(r io.Reader) io.Reader {
		read = 0
		return countingReader{r, &read}
	}})

	au, err := fileGetAudited(t, e, text)
	wantReason(t, wantCode(t, err, apperr.UnsupportedType), "content")
	if read > 12 {
		t.Errorf("中身が画像でないのに %d バイト読みました（先頭 12 バイトだけのはず）", read)
	}
	if !au.HasSize || au.Size != 2<<20 {
		t.Errorf("監査の size = %d (HasSize=%v), want ファイルの実サイズ", au.Size, au.HasSize)
	}

	// 画像なら全部読み、サイズを記録する。
	au, err = fileGetAudited(t, e, good)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(pngBytes) + 1<<20; read != want {
		t.Errorf("読んだバイト数 = %d, want ファイルの大きさ %d", read, want)
	}
	if !au.HasSize || au.Size != int64(len(pngBytes)+1<<20) {
		t.Errorf("成功時の監査の size = %d (HasSize=%v)", au.Size, au.HasSize)
	}
}

// stat で上限を超えていれば、その実サイズを監査に残す（中身は読まない）。
func TestFileGetTooLargeAuditsActualSize(t *testing.T) {
	e := newFileGetEnv(t)
	e.Cfg.FileGetMaxBytes = 64
	path := writeFile(t, filepath.Join(e.work, "over.png"), append(append([]byte{}, pngBytes...), make([]byte, 1000)...))
	au, err := fileGetAudited(t, e, path)
	wantCode(t, err, apperr.TooLarge)
	if !au.HasSize || au.Size != int64(len(pngBytes)+1000) {
		t.Errorf("監査の size = %d (HasSize=%v), want 実サイズ", au.Size, au.HasSize)
	}
}
