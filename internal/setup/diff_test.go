package setup

import (
	"reflect"
	"testing"
)

func loadBody(t *testing.T, body string) *State {
	t.Helper()
	st, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// 何も変えない入力は、編集操作を 1 つも作らない（既定値を書き出さない。FR-SET-3）。
func TestDiffNoChange(t *testing.T) {
	for name, body := range map[string]string{
		"手書き": handwritten(t),
		"最小":  "listen_addr = \"127.0.0.1:1\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			st := loadBody(t, body)
			if ops := Diff(st, answersOf(st), testHome); len(ops) != 0 {
				t.Fatalf("ops = %v", Keys(ops))
			}
		})
	}
}

// ひな形から始めて listen_addr だけ答えると、書くのは listen_addr だけ（denied_paths も sessions も書かない）。
func TestDiffNewFileWritesOnlyListenAddr(t *testing.T) {
	st, err := Load(t.TempDir() + "/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	ans := Defaults()
	ans.ListenAddr = "127.0.0.1:48120"
	if got := Keys(Diff(st, ans, testHome)); !reflect.DeepEqual(got, []string{"listen_addr"}) {
		t.Fatalf("keys = %v", got)
	}
	out, err := Render(st, Diff(st, ans, testHome))
	if err != nil {
		t.Fatal(err)
	}
	want := string(Template()) + "listen_addr = \"127.0.0.1:48120\"\n"
	if string(out) != want {
		t.Fatalf("新規は、ひな形の末尾に足すだけ:\n%s", out)
	}
}

func TestDiffChanges(t *testing.T) {
	const base = "listen_addr = \"127.0.0.1:1\"\n"
	tests := []struct {
		name string
		body string
		edit func(v *Values)
		want []string
	}{
		{"同じ値の書き直し（~ 展開後に同じ）", "listen_addr = \"127.0.0.1:1\"\nallowed_roots = [\"~/projects/\"]\n",
			func(v *Values) { v.AllowedRoots = []string{"/home/tester/projects"} }, nil},
		{"既定と同じ値は、ファイルに無ければ書かない", base,
			func(v *Values) {
				v.AllowedRoots = []string{"/mnt/nvme/", "~/projects/"}
				v.DeniedPaths = []string{"~/dotfiles"}
				v.Sessions = []string{"0"}
			}, nil},
		{"listen_addr は前後の空白があっても同じ値", "listen_addr = \"127.0.0.1:1\"\n",
			func(v *Values) { v.ListenAddr = " 127.0.0.1:1 " }, nil},
		{"listen_addr を変える", base, func(v *Values) { v.ListenAddr = "127.0.0.1:2" }, []string{"listen_addr"}},
		{"denied_paths を変える（既定と入れ替わる）", base,
			func(v *Values) { v.DeniedPaths = []string{"~/dotfiles", "/mnt/nvme/x"} }, []string{"denied_paths"}},
		{"denied_paths を空にする", base, func(v *Values) { v.DeniedPaths = nil }, []string{"denied_paths"}},
		{"ファイルにある値を既定に戻す", "listen_addr = \"127.0.0.1:1\"\nsessions = [\"a\"]\n",
			func(v *Values) { v.Sessions = []string{"0"} }, []string{"sessions"}},
		{"sessions を 2 つにして default_session を先頭のまま", base,
			func(v *Values) { v.Sessions = []string{"0", "w"}; v.DefaultSession = "0" }, []string{"sessions"}},
		{"sessions を 2 つにして default_session を選ぶ", base,
			func(v *Values) { v.Sessions = []string{"0", "w"}; v.DefaultSession = "w" }, []string{"sessions", "default_session"}},
		{"default_session だけ変える", "listen_addr = \"127.0.0.1:1\"\nsessions = [\"a\", \"b\"]\ndefault_session = \"a\"\n",
			func(v *Values) { v.DefaultSession = "b" }, []string{"default_session"}},
		{"sessions を 1 つに減らすと、書かれた default_session を合わせる", "listen_addr = \"127.0.0.1:1\"\nsessions = [\"a\", \"b\"]\ndefault_session = \"b\"\n",
			func(v *Values) { v.Sessions = []string{"a"} }, []string{"sessions", "default_session"}},
		{"sessions を 1 つに減らしても、default_session が無ければ書かない", "listen_addr = \"127.0.0.1:1\"\nsessions = [\"a\", \"b\"]\n",
			func(v *Values) { v.Sessions = []string{"a"} }, []string{"sessions"}},
		{"エージェントの command を足す", base,
			func(v *Values) { v.Agents["omp"] = Agent{Command: "omp --x"} }, []string{"agents.omp.command"}},
		{"エージェントの空欄は上書きしない（書かれたキーも消さない）", "listen_addr = \"127.0.0.1:1\"\n[agents.omp]\ncommand = \"omp --x\"\n",
			func(v *Values) { v.Agents["omp"] = Agent{} }, nil},
		{"エージェントの既定と同じ値は、ファイルに無ければ書かない", base,
			func(v *Values) { v.Agents["claude"] = AgentDefaults("claude") }, nil},
		{"auth_file は ~ 展開後に同じなら変更なし", "listen_addr = \"127.0.0.1:1\"\n[agents.claude]\nauth_file = \"~/x/auth.json\"\n",
			func(v *Values) { v.Agents["claude"] = Agent{AuthFile: "/home/tester/x/auth.json"} }, nil},
		{"型が違うキーは必ず書き直す", "listen_addr = \"127.0.0.1:1\"\nsessions = \"a\"\n",
			func(v *Values) { v.Sessions = []string{"a"} }, []string{"sessions"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := loadBody(t, tt.body)
			ans := answersOf(st)
			tt.edit(&ans)
			got := Keys(Diff(st, ans, testHome))
			if len(got) != len(tt.want) || (len(got) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Fatalf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}
