package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// FormatError は config.toml が TOML の構文として読めない・未知のキーがあるときのエラー。
// 値の検証エラーとは別で、`setup` はこれだと開かずに終了する（FR-SET-8）。
// メッセージには行番号・キー名が入っている。
type FormatError struct{ Err error }

func (e *FormatError) Error() string { return e.Err.Error() }
func (e *FormatError) Unwrap() error { return e.Err }

// ReadFile は config.toml を読む（サイズ上限つき。`serve` と同じ読み込みの入口）。
func ReadFile(path string) ([]byte, error) { return readLimited(path) }

// ParseBytes は config.toml の中身 data を、Load と同じ検証・既定値の適用・`~` 展開で Config にする。
// path は ConfigDir などの基準（ファイルは読まない）。`setup` が保存前に結果を読み直すために使う（FR-SET-5）。
// 構文エラー・未知のキーは *FormatError を返す（errors.As で取れる）。
func ParseBytes(data []byte, path string) (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config: ホームフォルダを決められません: %w", err)
	}
	return parse(data, path, home)
}

func parse(data []byte, path, home string) (*Config, error) {
	var raw rawConfig
	if err := decodeStrict(data, &raw); err != nil {
		return nil, fmt.Errorf("config: %s: %w", filepath.Base(path), err)
	}
	cfg, err := build(&raw, home)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", filepath.Base(path), err)
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("config: 設定フォルダを決められません: %w", err)
	}
	cfg.ConfigDir = dir
	cfg.TokensPath = filepath.Join(dir, "tokens.toml")
	cfg.TemplatesDir = filepath.Join(dir, "templates")
	cfg.ProfilesPath = filepath.Join(dir, "profiles.toml")
	cfg.NotesPath = filepath.Join(home, ".local", "share", "swarm-console", "notes.json")
	return cfg, nil
}
