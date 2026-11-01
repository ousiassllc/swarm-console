package setup

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/ousiassllc/swarm-console/internal/config"
)

//go:embed config.toml.example
var templateFS embed.FS

// Template は新規作成の出発点（全キーの説明と既定値をコメントで並べたひな形）。値を足すのは書き戻しの規則どおり。
func Template() []byte {
	b, err := templateFS.ReadFile("config.toml.example")
	if err != nil { // go:embed したファイルなので起きない
		panic(err)
	}
	return b
}

// ErrSymlink は config.toml がシンボリックリンクのとき（書かずに中止する。P-35）。
var ErrSymlink = errors.New("config.toml がシンボリックリンクです。rename でリンクが普通のファイルに変わり、リンク先が設定の置き場の外のことがあるため、書きません。手で編集してください")

// State は読み込んだ時点の config.toml。保存前の検査・競合検出の基準になる。
type State struct {
	Path   string
	Exists bool              // ファイルがあったか（無ければ Src はひな形）
	Src    []byte            // 読み込み時の内容（無ければひな形）
	Hash   [sha256.Size]byte // 読み込み時のファイルの内容のハッシュ（Exists のとき）
	Mode   os.FileMode       // 読み込み時の権限（Exists のとき）
	Config *config.Config    // serve と同じ読み込みの結果。値が不正なら nil
	// Invalid は値の検証エラー（Config が nil のとき）。開いて直させる（FR-SET-8）。
	Invalid error
	Cur     current
}

// Load は path の config.toml を読む。
//   - 無ければひな形から始める。
//   - シンボリックリンクなら ErrSymlink。
//   - TOML の構文エラー・未知のキーは *config.FormatError（行番号・キー名つき。開かない）。
//   - 値の検証エラーは State.Invalid に入れて返す（開いて直させる）。
func Load(path string) (*State, error) {
	st := &State{Path: path}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, ErrSymlink
		}
		st.Mode = fi.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s を調べられません: %w", path, err)
	}
	data, err := config.ReadFile(path)
	switch {
	case err == nil:
		st.Exists, st.Src, st.Hash = true, data, sha256.Sum256(data)
	case errors.Is(err, fs.ErrNotExist):
		st.Src = Template()
		st.Mode = 0o600
	default:
		return nil, fmt.Errorf("%s を読めません: %w", path, err)
	}
	cfg, err := config.ParseBytes(st.Src, path)
	var fe *config.FormatError
	switch {
	case errors.As(err, &fe):
		return nil, err
	case err != nil:
		st.Invalid = err
	default:
		st.Config = cfg
	}
	st.Cur = readCurrent(st.Src)
	return st, nil
}
