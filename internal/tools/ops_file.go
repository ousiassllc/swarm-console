package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/safety"
)

// 取得できる種類。拡張子（小文字）→ MIME。中身の先頭バイトも同じ MIME でなければ断る。
var fileGetMIME = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".gif": "image/gif", ".mp4": "video/mp4",
}

type fileGetArgs struct {
	Path *string `json:"path"`
}

type fileGetJSON struct {
	Path       string `json:"path"`
	MIME       string `json:"mime"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	DataBase64 string `json:"data_base64"`
}

func (e *env) fileGetTool() Tool {
	return Tool{
		Name:        "file_get",
		Description: descFileGet,
		Scope:       ScopeOps,
		Schema: object([]string{"path"}, props{
			"path": strProp("サーバー上のファイルの絶対パス（png・jpg・jpeg・webp・gif・mp4。ペインの cwd の下・レポート置き場・設定で足した根の下だけ）"),
		}),
		Handler: e.fileGet,
	}
}

func (e *env) fileGet(ctx context.Context, c auth.Client, raw json.RawMessage) (Result, error) {
	var a fileGetArgs
	if err := decodeArgs(raw, &a); err != nil {
		return Result{}, err
	}
	path, err := requireStr(a.Path, "path")
	if err != nil {
		return Result{}, err
	}
	au := auditOf(ctx)
	au.setPath(path)
	return e.readMedia(ctx, c, au, path)
}

// readMedia は path の検証・読み取り・種類の確認をして file_get の結果にする。
// 順序: 許可・秘密（CheckFileGet）→ 拡張子 → 通常ファイルか → サイズ → 読み取り → 先頭バイト。
func (e *env) readMedia(ctx context.Context, c auth.Client, au *Audit, path string) (Result, error) {
	roots, err := e.fileGetRoots(ctx, c)
	if err != nil {
		return Result{}, err
	}
	resolved, err := e.Safety.CheckFileGet(path, roots)
	if err != nil {
		return Result{}, apperr.As(err)
	}
	au.setPath(resolved)
	want, ok := fileGetMIME[strings.ToLower(filepath.Ext(resolved))]
	if !ok {
		return Result{}, apperr.New(apperr.UnsupportedType, "reason", "extension")
	}
	data, err := e.readBounded(au, resolved)
	if err != nil {
		return Result{}, err
	}
	if sniffMIME(data) != want {
		return Result{}, apperr.New(apperr.UnsupportedType, "reason", "content")
	}
	sum := sha256.Sum256(data)
	return marshalResult(fileGetJSON{
		Path: resolved, MIME: want, Size: int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]), DataBase64: base64.StdEncoding.EncodeToString(data),
	})
}

// fileGetRoots は file_get が読んでよい根。c が触れるセッションのペインの cwd・レポート置き場・設定の file_get_roots。
// ペインの cwd が / やホームそのもの（実パスで判定）のときは根に入れない。明示の根（レポート置き場・file_get_roots）はそのまま。
func (e *env) fileGetRoots(ctx context.Context, c auth.Client) ([]string, error) {
	snap, err := e.Registry.Current(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	roots := append([]string{e.Cfg.ReportDir}, e.Cfg.FileGetRoots...)
	home, _ := os.UserHomeDir() // 取れなければ "" のままで、"/" だけを除く
	allowed := e.allowed(c)
	for i := range snap.Panes {
		p := &snap.Panes[i]
		if slices.Contains(allowed, p.Session) && filepath.IsAbs(p.Cwd) && !slices.Contains(roots, p.Cwd) &&
			!safety.IsBroadCWD(p.Cwd, home) {
			roots = append(roots, p.Cwd)
		}
	}
	return roots, nil
}

// readBounded は通常ファイルを上限以内で全部読む。stat で先に判定し、読み込みも上限＋1 バイトで切る
// （stat のあとに大きくされても上限を超えて読まない）。通常ファイル以外（ディレクトリ・FIFO・デバイス）は unsupported_type。
func (e *env) readBounded(au *Audit, resolved string) ([]byte, error) {
	limit := e.Cfg.FileGetMaxBytes
	if st, err := os.Lstat(resolved); err != nil {
		return nil, openError(err)
	} else if !st.Mode().IsRegular() {
		return nil, apperr.New(apperr.UnsupportedType, "reason", "not_regular")
	}
	// O_NONBLOCK: 検証後に FIFO へ差し替えられても open で固まらない。O_NOFOLLOW: 検証後のリンク差し替えを拒否。
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0) //nolint:gosec // CheckFileGet 済みの実パス
	if err != nil {
		return nil, openError(err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	if !st.Mode().IsRegular() {
		return nil, apperr.New(apperr.UnsupportedType, "reason", "not_regular")
	}
	if err := safety.CheckOpened(f, resolved); err != nil {
		return nil, apperr.As(err)
	}
	au.setSize(st.Size())
	if st.Size() > limit {
		return nil, apperr.New(apperr.TooLarge, "max", limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, apperr.Wrap(apperr.Internal, err)
	}
	au.setSize(int64(len(data)))
	if int64(len(data)) > limit {
		return nil, apperr.New(apperr.TooLarge, "max", limit)
	}
	return data, nil
}

// openError は検証後に起きた open・stat の失敗を API のコードにする。
func openError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return apperr.New(apperr.NotFound, "field", "path")
	case errors.Is(err, syscall.ELOOP), errors.Is(err, os.ErrPermission):
		return apperr.New(apperr.PathForbidden, "field", "path", "reason", "unreadable")
	}
	return apperr.Wrap(apperr.Internal, err)
}

// sniffMIME は先頭バイトから MIME を決める（PNG・JPEG・WebP・GIF・MP4）。当てはまらなければ ""。
func sniffMIME(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case len(b) >= 12 && bytes.Equal(b[4:8], []byte("ftyp")):
		return "video/mp4"
	}
	return ""
}
