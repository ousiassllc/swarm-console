package setup

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/agent"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/config/tomledit"
	"github.com/ousiassllc/swarm-console/internal/store"
)

// ErrConflict は、読み込んだあとに config.toml が変わっていたとき（serve や手書きとの競合。上書きしない）。
var ErrConflict = errors.New("config.toml が読み込んだあとに変わっています。上書きせず中止します。もう一度 setup を実行してください")

// Render は st の内容に ops を適用し、保存前の検査（ER §5.1）をすべて通った結果のバイト列を返す。
// 1 つでも落ちたらエラー（何も書かない）。ファイルには触れない。
//
//	① 結果を serve と同じ読み込み経路（config.ParseBytes）で読み直して通る
//	② 値を変えていないキーの実効値が読み込み時と同じ
//	③ 置き換えた範囲・足した行の外のバイト列が元と同一（tomledit.Edit が確かめる）
func Render(st *State, ops []tomledit.Op) ([]byte, error) {
	out, err := tomledit.Edit(st.Src, ops)
	if err != nil {
		return nil, err
	}
	after, err := config.ParseBytes(out, st.Path)
	if err != nil {
		return nil, fmt.Errorf("結果が serve の読み込みを通りません（何も書きません）: %w", err)
	}
	if st.Config != nil {
		if err := sameUntouched(st.Config, after, ops); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// sameUntouched は、ops が触れていないキーの実効値が before と after で同じことを確かめる。
func sameUntouched(before, after *config.Config, ops []tomledit.Op) error {
	b, a := *before, *after
	for _, op := range ops {
		switch strings.Join(op.Key, ".") {
		case keyListen:
			b.ListenAddr, a.ListenAddr = "", ""
		case keySess:
			b.Sessions, a.Sessions = nil, nil
			b.DefaultSession, a.DefaultSession = "", ""
		case keyDefault:
			b.DefaultSession, a.DefaultSession = "", ""
		case keyAllowed:
			b.AllowedRoots, a.AllowedRoots = nil, nil
			// file_roots の既定は allowed_roots から決まる（Load と同じ）。file_roots を書いていなければ一緒に変わる。
			b.FileRoots, a.FileRoots = nil, nil
		case keyDenied:
			b.DeniedPaths, a.DeniedPaths = nil, nil
		default:
			maskAgent(&b, &a, op.Key)
		}
	}
	b.Agents, a.Agents = dropEmptyAgents(b.Agents), dropEmptyAgents(a.Agents)
	if !reflect.DeepEqual(b, a) {
		return errors.New("変えていないキーの値が読み込み時と変わります（何も書きません）")
	}
	return nil
}

// dropEmptyAgents は、上書きが 1 つも無い（すべて nil）エントリを外す。
// 「[agents.omp] が無い」と「空の [agents.omp]」は上書きが無い点で同じなので、比較では区別しない。
func dropEmptyAgents(in map[string]agent.Overrides) map[string]agent.Overrides {
	var out map[string]agent.Overrides
	for k, v := range in {
		if reflect.DeepEqual(v, agent.Overrides{}) {
			continue
		}
		if out == nil {
			out = map[string]agent.Overrides{}
		}
		out[k] = v
	}
	return out
}

// maskAgent は agents.<name>.<field> の 1 フィールドを比較から外す（エントリが無い側は空の上書きとして扱う）。
func maskAgent(b, a *config.Config, key []string) {
	if len(key) != 3 || key[0] != "agents" {
		return
	}
	for _, c := range []*config.Config{b, a} {
		ov := c.Agents[key[1]]
		switch key[2] {
		case "command":
			ov.Command = nil
		case "exit_command":
			ov.ExitCommand = nil
		case "auth_file":
			ov.AuthFile = nil
		}
		m := make(map[string]agent.Overrides, len(c.Agents))
		for k, v := range c.Agents {
			m[k] = v
		}
		m[key[1]] = ov
		c.Agents = m
	}
}

// Save は out（Render の結果）を st.Path に書く。
// 書く直前にシンボリックリンクでないこと・読み込み後に変わっていないことを確かめる。
// 一時ファイル → fsync → rename。新規は 0600、既存は権限を変えない（P-39）。
func Save(st *State, out []byte) error {
	fi, err := os.Lstat(st.Path)
	switch {
	case err == nil && fi.Mode()&fs.ModeSymlink != 0:
		return ErrSymlink
	case err == nil && !st.Exists, errors.Is(err, fs.ErrNotExist) && st.Exists:
		return ErrConflict
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s を調べられません: %w", st.Path, err)
	}
	if st.Exists {
		now, err := config.ReadFile(st.Path)
		if err != nil {
			return fmt.Errorf("%s を読み直せません: %w", st.Path, err)
		}
		if sha256.Sum256(now) != st.Hash {
			return ErrConflict
		}
	}
	perm := os.FileMode(0o600)
	if st.Exists {
		perm = fi.Mode().Perm()
	}
	if err := store.WriteFileAtomic(st.Path, out, perm); err != nil {
		return fmt.Errorf("%s を保存できません: %w", st.Path, err)
	}
	return nil
}

// Unchanged は out が読み込み時の内容と同じか（書く必要がないか）。
func Unchanged(st *State, out []byte) bool { return st.Exists && bytes.Equal(st.Src, out) }
