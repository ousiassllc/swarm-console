package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// lockSuffix は tokens.toml のロックファイル名（tokens.toml と同じフォルダ）に付ける接尾辞。
const lockSuffix = ".lock"

// lockTokens は tokens.toml の「読む → 変更 → 書く」を直列化するロックを取り、解くための関数を返す。
// 同じプロセス内は manageMu、別プロセス（token add・revoke・pair・setup）は
// ロックファイル（<path>.lock、権限 600）への flock(LOCK_EX) で直列化する。
// ロックを取れなければ unlock は nil でエラーを返す（呼び出し側は何も書かない）。
//
// create が true ならフォルダ（700）とロックファイルを無ければ作る（Add）。
// false でフォルダが無ければ、tokens.toml もあり得ないのでロックなしで通す（Revoke がフォルダを作らないため。
// 読んだあとの readRecords が「トークンが無い」を返す）。フォルダがあれば Revoke は書く経路なので、
// ロックファイルは無ければ作り、取れなければ失敗する。読むだけの List はロックを取らず、lockTokens を通らない。
func lockTokens(path string, create bool) (unlock func(), err error) {
	manageMu.Lock()
	f, err := openLockFile(path+lockSuffix, create)
	if err != nil {
		manageMu.Unlock()
		return nil, err
	}
	if f == nil {
		return manageMu.Unlock, nil
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		manageMu.Unlock()
		return nil, fmt.Errorf("tokens.toml のロックを取れません: %w", err)
	}
	return func() {
		_ = f.Close() // fd を閉じるとロックも解ける
		manageMu.Unlock()
	}, nil
}

// openLockFile はロックファイルを開く。create が false でフォルダが無ければ (nil, nil)。
func openLockFile(lockPath string, create bool) (*os.File, error) {
	dir := filepath.Dir(lockPath)
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // tokens.toml と同じフォルダ（呼び出し元が決めたパス）
			return nil, fmt.Errorf("tokens.toml のロックを取れません: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // tokens.toml と同じフォルダの固定名
	if err != nil {
		if !create && errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("tokens.toml のロックを取れません: %w", err)
	}
	return f, nil
}
