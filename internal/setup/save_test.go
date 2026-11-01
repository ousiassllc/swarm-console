package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/setup/template"

	"github.com/ousiassllc/swarm-console/internal/config/tomledit"
)

// 手書きのサンプルで 1 項目だけ変えると、その値の範囲以外は 1 バイトも変わらない（FR-SET-3）。
func TestRenderHandwrittenOneChange(t *testing.T) {
	src := handwritten(t)
	tests := []struct {
		name string
		edit func(v *Values)
		old  string
		new  string
	}{
		{"listen_addr", func(v *Values) { v.ListenAddr = "127.0.0.1:50000" }, `"127.0.0.1:48120"`, `"127.0.0.1:50000"`},
		{"sessions（値の前の空白も残る）", func(v *Values) { v.Sessions = []string{"0", "w"}; v.DefaultSession = "0" }, `["0"]`, `["0", "w"]`},
		{"エージェントの command", func(v *Values) { v.Agents["omp"] = Agent{Command: "omp --slow"} }, `"omp --fast"`, `"omp --slow"`},
		{"dotted key の command", func(v *Values) { v.Agents["codex"] = Agent{Command: "codex"} }, `"codex --yolo"`, `"codex"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := loadBody(t, src)
			ans := answersOf(st)
			tt.edit(&ans)
			out, err := Render(st, Diff(st, ans, testHome))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(src, tt.old, tt.new, 1)
			if string(out) != want {
				t.Fatalf("変えた値の範囲の外が変わっています\n got: %q\nwant: %q", out, want)
			}
		})
	}
}

// 手書きに無いキーを足す: 最上位は最初の表の直前、表の中は最後のキーの次の行。
func TestRenderHandwrittenAddKeys(t *testing.T) {
	src := handwritten(t)
	st := loadBody(t, src)
	ans := answersOf(st)
	ans.DeniedPaths = []string{"~/dotfiles", "/mnt/nvme/secret"}
	ans.Agents["claude"] = Agent{Command: "claude --x"}
	out, err := Render(st, Diff(st, ans, testHome))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "agents.codex.command = \"codex --yolo\"   # dotted key\n",
		"agents.codex.command = \"codex --yolo\"   # dotted key\ndenied_paths = [\"~/dotfiles\", \"/mnt/nvme/secret\"]\n", 1)
	want = strings.Replace(want, "[agents.claude]\nexit_command = \"/exit\"\n", "[agents.claude]\nexit_command = \"/exit\"\ncommand = \"claude --x\"\n", 1)
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// コメント入りの複数行配列は、了承なしでは置き換えない（tomledit の約束。setup は確認を挟む）。
func TestRenderCommentedArrayNeedsConsent(t *testing.T) {
	st := loadBody(t, handwritten(t))
	ans := answersOf(st)
	ans.AllowedRoots = []string{"/mnt/nvme/"}
	ops := Diff(st, ans, testHome)
	_, err := Render(st, ops)
	var cl *tomledit.CommentLossError
	if !errors.As(err, &cl) {
		t.Fatalf("err = %v", err)
	}
	ops[0].AllowCommentLoss = true
	out, err := Render(st, ops)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "allowed_roots = [\"/mnt/nvme/\"]\n\nfile_roots") {
		t.Fatalf("out:\n%s", out)
	}
}

// 保存前の検査: 結果が serve の読み込みで落ちるなら何も返さない。
func TestRenderRefusesInvalidResult(t *testing.T) {
	st := loadBody(t, "listen_addr = \"127.0.0.1:1\"\nsessions = [\"a\", \"b\"]\ndefault_session = \"b\"\n")
	// default_session が sessions から外れる編集（画面は合わせるが、検査が最後の砦）
	ops := []tomledit.Op{{Key: []string{"sessions"}, Value: []string{"a", "c"}}}
	if out, err := Render(st, ops); err == nil || !strings.Contains(err.Error(), "何も書きません") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	bad := []tomledit.Op{{Key: []string{"listen_addr"}, Value: "0.0.0.0:1"}}
	if _, err := Render(st, bad); err == nil {
		t.Fatal("0.0.0.0 は書けない")
	}
}

// 変えていないキーの実効値が変わる編集は書かない（② の検査）。
func TestRenderRefusesChangedUntouchedKeys(t *testing.T) {
	st := loadBody(t, "listen_addr = \"127.0.0.1:1\"\ndefault_lang = \"ja\"\n")
	// default_lang を ops で触っていないのに値が変わる、は tomledit では作れないので、sameUntouched を直接確かめる。
	changed := *st.Config
	changed.DefaultLang = "en"
	if err := sameUntouched(st.Config, &changed, nil); err == nil {
		t.Fatal("触っていないキーの変化を見逃した")
	}
	if err := sameUntouched(st.Config, st.Config, []tomledit.Op{{Key: []string{"listen_addr"}}}); err != nil {
		t.Fatal(err)
	}
	// allowed_roots を変えると file_roots の既定も動く。これは許す。
	st2 := loadBody(t, "listen_addr = \"127.0.0.1:1\"\n")
	out, err := Render(st2, []tomledit.Op{{Key: []string{"allowed_roots"}, Value: []string{"/srv/x"}}})
	if err != nil || !strings.Contains(string(out), "allowed_roots") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestSaveNewFilePermAndDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	out := append(template.Bytes(), "listen_addr = \"127.0.0.1:1\"\n"...)
	if err := Save(st, out); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("新規は 600: %v %v", fi, err)
	}
	if b := readFile(t, path); !bytes.Equal(b, out) {
		t.Fatal("内容が違います")
	}
}

func TestSaveKeepsExistingPerm(t *testing.T) {
	path := writeConfig(t, "listen_addr = \"127.0.0.1:1\"\n")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(st, []byte("listen_addr = \"127.0.0.1:2\"\n")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o400 {
		t.Fatalf("既存の権限は変えない: %v", fi.Mode().Perm())
	}
}

// 読み込んだあとにファイルが変わっていたら、上書きせず中止する。
func TestSaveConflict(t *testing.T) {
	path := writeConfig(t, "listen_addr = \"127.0.0.1:1\"\n")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	const other = "listen_addr = \"127.0.0.1:9\" # 手書きで直した\n"
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(st, []byte("listen_addr = \"127.0.0.1:2\"\n")); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if b := readFile(t, path); string(b) != other {
		t.Fatal("競合したのに上書きしました")
	}
}

func TestSaveConflictCreatedMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	const other = "listen_addr = \"127.0.0.1:9\"\n"
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(st, []byte("listen_addr = \"127.0.0.1:2\"\n")); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if b := readFile(t, path); string(b) != other {
		t.Fatal("上書きしました")
	}
}

func TestSaveRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.toml")
	const body = "listen_addr = \"127.0.0.1:1\"\n"
	if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	st, err := Load(path) // まだ無い
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := Save(st, []byte("listen_addr = \"127.0.0.1:2\"\n")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v", err)
	}
	if b := readFile(t, target); string(b) != body {
		t.Fatal("リンク先に書きました")
	}
	if fi, _ := os.Lstat(path); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("リンクが普通のファイルに変わりました")
	}
}

// まだ [agents.<name>] が無いエージェントの設定を初めて書いても保存できる。範囲外のバイト列は変わらない。
func TestRenderFirstAgentSetting(t *testing.T) {
	tests := []struct {
		name, src string
		want      string
	}{
		{
			name: "agents の表が 1 つも無いファイル",
			src:  "# 手書き\nlisten_addr = \"127.0.0.1:1\"\n",
			want: "# 手書き\nlisten_addr = \"127.0.0.1:1\"\n\n[agents.omp]\ncommand = \"omp --x\"\n",
		},
		{
			name: "別のエージェントの表だけがあるファイル",
			src:  "listen_addr = \"127.0.0.1:1\"\n\n[agents.claude]\nexit_command = \"/exit\" # c\n",
			want: "listen_addr = \"127.0.0.1:1\"\n\n[agents.claude]\nexit_command = \"/exit\" # c\n\n[agents.omp]\ncommand = \"omp --x\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := loadBody(t, tt.src)
			ans := answersOf(st)
			ans.Agents["omp"] = Agent{Command: "omp --x"}
			out, err := Render(st, Diff(st, ans, testHome))
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.want {
				t.Fatalf("got %q\nwant %q", out, tt.want)
			}
			if err := Save(st, out); err != nil {
				t.Fatal(err)
			}
		})
	}
}
