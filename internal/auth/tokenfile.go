package auth

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"

	"github.com/ousiassllc/swarm-console/internal/store"
)

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// record は tokens.toml の [[token]] 1 件（ER §3）。フィールド宣言順がそのまま書き出しのキー順。
type record struct {
	Name      string `toml:"name"`
	Scope     string `toml:"scope"`
	Hash      string `toml:"hash"`
	CreatedAt string `toml:"created_at"`
	RevokedAt string `toml:"revoked_at,omitempty"`
}

type tokenFile struct {
	Token []record `toml:"token"`
}

func validScope(s Scope) bool { return s == ScopeReport || s == ScopeOps }

// readRecords は path を検証して読む。ファイルが無ければ空。
// 権限が 600 でない・TOML 不正・未知キー・name/scope/hash の形式違反・name 重複はエラー。
// エラーにハッシュの値は含めない。
func readRecords(path string) ([]record, error) {
	if err := store.CheckMode(path, 0o600); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // 呼び出し元が決めた tokens.toml のパス
	if err != nil {
		return nil, fmt.Errorf("tokens.toml を読めません: %w", err)
	}
	var f tokenFile
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("tokens.toml の TOML が不正です: %w", err)
	}
	seen := make(map[string]struct{}, len(f.Token))
	for i, r := range f.Token {
		switch {
		case !namePattern.MatchString(r.Name):
			return nil, fmt.Errorf("tokens.toml の %d 件目: name の形式が不正です", i+1)
		case !validScope(Scope(r.Scope)):
			return nil, fmt.Errorf("tokens.toml の %d 件目（%s）: scope が未知です", i+1, r.Name)
		case !hashPattern.MatchString(r.Hash):
			return nil, fmt.Errorf("tokens.toml の %d 件目（%s）: hash の形式が不正です", i+1, r.Name)
		}
		if _, dup := seen[r.Name]; dup {
			return nil, fmt.Errorf("tokens.toml の name が重複しています: %s", r.Name)
		}
		seen[r.Name] = struct{}{}
	}
	return f.Token, nil
}

// writeRecords は recs を一時ファイル → rename、権限 600 で書く。
func writeRecords(path string, recs []record) error {
	data, err := toml.Marshal(tokenFile{Token: recs})
	if err != nil {
		return fmt.Errorf("tokens.toml を組み立てられません: %w", err)
	}
	return store.WriteFileAtomic(path, data, 0o600)
}
