package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/ousiassllc/swarm-console/internal/config"
)

func TestLoadHandwritten(t *testing.T) {
	path := writeConfig(t, handwritten(t))
	st, err := Load(path)
	if err != nil {
		t.Fatalf("手書きのサンプルを開けません: %v", err)
	}
	if !st.Exists || st.Invalid != nil || st.Config == nil {
		t.Fatalf("State = %+v", st)
	}
	v := st.Cur.Values
	if v.ListenAddr != "127.0.0.1:48120" || len(v.AllowedRoots) != 2 || v.AllowedRoots[1] != "~/projects/" {
		t.Fatalf("現在値 = %+v", v)
	}
	if got := v.Agents["omp"].Command; got != "omp --fast" {
		t.Fatalf("omp command = %q", got)
	}
	if got := v.Agents["codex"].Command; got != "codex --yolo" {
		t.Fatalf("dotted key の値を読めていません: %q", got)
	}
	if st.Cur.Keys[keyDenied].Present {
		t.Fatal("denied_paths は書かれていない")
	}
	if len(v.DeniedPaths) != 1 || v.DeniedPaths[0] != "~/dotfiles" {
		t.Fatalf("書かれていない denied_paths は既定が初期値: %v", v.DeniedPaths)
	}
}

func TestLoadMissingStartsFromTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Exists || string(st.Src) != string(Template()) {
		t.Fatal("無ければひな形から始める")
	}
	if st.Invalid == nil || !strings.Contains(st.Invalid.Error(), "listen_addr") {
		t.Fatalf("ひな形だけでは listen_addr 未設定: %v", st.Invalid)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("開いただけでファイルを作ってはいけない")
	}
}

// ひな形は全部コメントで、手で使うには listen_addr だけ足せばよい（ER §5.1 新規作成）。
func TestTemplateIsAllComments(t *testing.T) {
	var doc map[string]any
	if err := toml.Unmarshal(Template(), &doc); err != nil || len(doc) != 0 {
		t.Fatalf("ひな形に有効なキーがあります: %v %v", doc, err)
	}
	if _, err := config.ParseBytes(append(Template(), "listen_addr = \"127.0.0.1:1\"\n"...), "config.toml"); err != nil {
		t.Fatalf("ひな形 + listen_addr が読み込みを通りません: %v", err)
	}
}

// 構文エラー・未知のキーは開かない（行番号・キー名つき）。値の不正・listen_addr 未設定は開く（FR-SET-8）。
func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name, body string
		wantErr    string // 空なら開ける
	}{
		{"構文エラー", "listen_addr = \"127.0.0.1:1\"\nsessions = [\n", "TOML を読めません"},
		{"未知のキー", "listen_addr = \"127.0.0.1:1\"\nmystery = 1\n", "mystery（2 行目）"},
		{"listen_addr 未設定", "sessions = [\"0\"]\n", ""},
		{"listen_addr が不正", "listen_addr = \"0.0.0.0:48120\"\n", ""},
		{"型が違う値", "listen_addr = \"127.0.0.1:1\"\nsessions = \"a\"\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.body)
			st, err := Load(path)
			if tt.wantErr != "" {
				var fe *config.FormatError
				if !errors.As(err, &fe) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || st.Invalid == nil {
				t.Fatalf("開いて直させる: st=%+v err=%v", st, err)
			}
		})
	}
}

func TestLoadSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.toml")
	if err := os.WriteFile(target, []byte("listen_addr = \"127.0.0.1:1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadCurrentLenient(t *testing.T) {
	c := readCurrent([]byte("listen_addr = 1\nsessions = [1, 2]\nallowed_roots = \"x\"\n[agents.omp]\ncommand = 3\nexit_command = \"/q\"\n"))
	for _, k := range []string{keyListen, keySess, keyAllowed, agentKey("omp", "command")} {
		if st := c.Keys[k]; !st.Present || !st.Bad {
			t.Errorf("%s は書かれているが型が違う: %+v", k, st)
		}
	}
	if st := c.Keys[agentKey("omp", "exit_command")]; !st.Present || st.Bad {
		t.Errorf("exit_command = %+v", st)
	}
	if c.Values.Agents["omp"].ExitCommand != "/q" {
		t.Errorf("exit_command = %q", c.Values.Agents["omp"].ExitCommand)
	}
}
