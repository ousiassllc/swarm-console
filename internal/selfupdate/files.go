// Package selfupdate は serve の自己更新を担う：許可ホスト内のダウンロード・SHA256SUMS の検証・
// ~/bin/swarm-console/ の 5 ファイルへの入れ替え・起動時の試運転と戻し（FR-UPD-11〜20）。
// 新しいバイナリは入れ替える前に実行せず、systemctl などのコマンドも実行しない（NFR-SEC-16）。
package selfupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/swarm-console/internal/store"
	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// 置き場 ~/bin/swarm-console/ の 5 ファイル（FR-UPD-15。これ以外には書かない。NFR-SEC-14）。
const (
	binName     = "swarm-console"
	newName     = binName + ".new"
	oldName     = binName + ".old"
	pendingName = binName + ".update-pending"
	failedName  = binName + ".update-failed"
)

// Dir は home から決める置き場（$HOME/bin/swarm-console）。
func Dir(home string) string { return filepath.Join(home, "bin", "swarm-console") }

// pending は swarm-console.update-pending（試運転中のマーカー）。
type pending struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Boots int    `json:"boots"`
}

// failed は swarm-console.update-failed（起動に失敗して戻した版）。
type failed struct {
	Version string `json:"version"`
	At      string `json:"at"`
}

// readJSON は path を strict に読む。無い・読めない・形が違う（未知のキーを含む）なら ok=false。
// 形が違うファイルは存在していれば true を返す（呼び出し側が消す。ER §8）。
func readJSON(path string, v any) (exists, ok bool) {
	b, err := os.ReadFile(path) //nolint:gosec // Dir(home) 配下の固定名
	if err != nil {
		return !errors.Is(err, os.ErrNotExist), false
	}
	return true, decodeStrict(b, v)
}

func decodeStrict(b []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil && !dec.More()
}

// readPending はマーカーを読む。形が不正（版の形・boots）ならファイルを消して ok=false。
func readPending(dir string) (p pending, exists, ok bool) {
	path := filepath.Join(dir, pendingName)
	exists, ok = readJSON(path, &p)
	if ok && (!updatecheck.ValidTag(p.From) || !updatecheck.ValidTag(p.To) || p.Boots < 0) {
		ok = false
	}
	if exists && !ok {
		_ = os.Remove(path)
	}
	return p, exists, ok
}

func writePending(dir string, p pending) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(filepath.Join(dir, pendingName), b, 0o600)
}

// readFailed は戻した版を読む。形が不正なら無いものとして消す。
func readFailed(dir string) (version string, ok bool) {
	path := filepath.Join(dir, failedName)
	var f failed
	exists, ok := readJSON(path, &f)
	if ok && !updatecheck.ValidTag(f.Version) {
		ok = false
	}
	if exists && !ok {
		_ = os.Remove(path)
	}
	return f.Version, ok
}

func writeFailed(dir, version string, at time.Time) error {
	b, err := json.Marshal(failed{Version: version, At: at.Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(filepath.Join(dir, failedName), b, 0o600)
}

// removeIfExists は無いファイルを成功扱いで消す。
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}
