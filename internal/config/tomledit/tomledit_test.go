package tomledit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/config/tomledit"
)

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

func op(v any, key ...string) tomledit.Op { return tomledit.Op{Key: key, Value: v} }

// 値を変える・キーを足す。結果は want と 1 バイトも違わない（変更範囲の外は元のまま）。
// noLoad でなければ、結果を serve と同じ読み込み（config.ParseBytes）に通して落ちないことも確かめる。
func TestEdit(t *testing.T) {
	const base = "# 手書きの設定\nlisten_addr = \"100.64.0.1:48120\"   # 行末のコメント\nsessions = [\"0\"]\n\n"
	tests := []struct {
		name   string
		src    string
		ops    []tomledit.Op
		want   string
		noLoad bool
	}{
		{
			name: "値の範囲だけ置き換える（空白・行末コメントは残る）",
			src:  "listen_addr   =   \"100.64.0.1:48120\"  # keep\nsessions = [\"0\"]\n",
			ops:  []tomledit.Op{op("127.0.0.1:1", "listen_addr")},
			want: "listen_addr   =   \"127.0.0.1:1\"  # keep\nsessions = [\"0\"]\n",
		},
		{
			name: "CRLF のファイルでも他の行は変わらない",
			src:  crlf(base),
			ops:  []tomledit.Op{op([]string{"a", "b"}, "sessions")},
			want: crlf("# 手書きの設定\nlisten_addr = \"100.64.0.1:48120\"   # 行末のコメント\nsessions = [\"a\", \"b\"]\n\n"),
		},
		{
			name: "複数行の配列（コメントなし）は 1 行の配列になる",
			src:  "listen_addr = \"127.0.0.1:1\"\nallowed_roots = [\n  \"/a\",\n  \"/b\",\n]\nsessions = [\"0\"]\n",
			ops:  []tomledit.Op{op([]string{"/c"}, "allowed_roots")},
			want: "listen_addr = \"127.0.0.1:1\"\nallowed_roots = [\"/c\"]\nsessions = [\"0\"]\n",
		},
		{
			name: "表の中のキーを置き換える",
			src:  "listen_addr = \"127.0.0.1:1\"\n\n[agents.omp]\ncommand = \"omp\" # c\nexit_command = \"/exit\"\n",
			ops:  []tomledit.Op{op("omp --x", "agents", "omp", "command")},
			want: "listen_addr = \"127.0.0.1:1\"\n\n[agents.omp]\ncommand = \"omp --x\" # c\nexit_command = \"/exit\"\n",
		},
		{
			name: "dotted key の値も置き換える",
			src:  "listen_addr = \"127.0.0.1:1\"\nagents.omp.command = \"omp\"\n",
			ops:  []tomledit.Op{op("x", "agents", "omp", "command")},
			want: "listen_addr = \"127.0.0.1:1\"\nagents.omp.command = \"x\"\n",
		},
		{
			name: "引用符つきのキー・別の表の同名キーは取り違えない",
			src:  "listen_addr = \"127.0.0.1:1\"\n[pane_names]\n\"0:1.2\" = \"build\"\n[agents.omp]\ncommand = \"omp\"\n[agents.claude]\ncommand = \"claude\"\n",
			ops:  []tomledit.Op{op("c2", "agents", "claude", "command")},
			want: "listen_addr = \"127.0.0.1:1\"\n[pane_names]\n\"0:1.2\" = \"build\"\n[agents.omp]\ncommand = \"omp\"\n[agents.claude]\ncommand = \"c2\"\n",
		},
		{
			name: "最上位のキーは最初の表ヘッダーの直前に足す（直前の空行は保つ）",
			src:  "listen_addr = \"127.0.0.1:1\"\n\n[pane_names]\n\"0:1.2\" = \"build\"\n",
			ops:  []tomledit.Op{op([]string{"/r"}, "allowed_roots")},
			want: "listen_addr = \"127.0.0.1:1\"\nallowed_roots = [\"/r\"]\n\n[pane_names]\n\"0:1.2\" = \"build\"\n",
		},
		{
			name: "表の説明コメントとヘッダーの間には入らない",
			src:  "listen_addr = \"127.0.0.1:1\"\n\n# 固定名\n[pane_names]\n\"0:1.2\" = \"build\"\n",
			ops:  []tomledit.Op{op(true, "x_flag")},
			want: "listen_addr = \"127.0.0.1:1\"\nx_flag = true\n\n# 固定名\n[pane_names]\n\"0:1.2\" = \"build\"\n",
			// x_flag は未知のキーなので serve は拒否する。編集部品は壊さないことだけ確かめる。
			noLoad: true,
		},
		{
			name: "表が無ければ最上位のキーは末尾に足す",
			src:  "# c\nlisten_addr = \"127.0.0.1:1\"\n\n# end\n",
			ops:  []tomledit.Op{op([]string{"x"}, "sessions")},
			want: "# c\nlisten_addr = \"127.0.0.1:1\"\n\n# end\nsessions = [\"x\"]\n",
		},
		{
			name: "末尾の改行が無いファイルは、足した最後の行も改行なし",
			src:  "listen_addr = \"127.0.0.1:1\"",
			ops:  []tomledit.Op{op([]string{"x"}, "sessions"), op([]string{"/r"}, "allowed_roots")},
			want: "listen_addr = \"127.0.0.1:1\"\nsessions = [\"x\"]\nallowed_roots = [\"/r\"]",
		},
		{
			name: "空のファイルは足した行だけ",
			src:  "",
			ops:  []tomledit.Op{op("127.0.0.1:1", "listen_addr")},
			want: "listen_addr = \"127.0.0.1:1\"\n",
		},
		{
			name: "CRLF のファイルには CRLF で足す",
			src:  crlf("listen_addr = \"127.0.0.1:1\"\n\n[agents.omp]\ncommand = \"omp\"\n"),
			ops:  []tomledit.Op{op([]string{"x"}, "sessions"), op("/e", "agents", "omp", "exit_command")},
			want: crlf("listen_addr = \"127.0.0.1:1\"\nsessions = [\"x\"]\n\n[agents.omp]\ncommand = \"omp\"\nexit_command = \"/e\"\n"),
		},
		{
			name: "既にある表の最後のキーの次の行に足す",
			src:  "listen_addr = \"127.0.0.1:1\"\n[agents.omp]\ncommand = \"omp\" # c\n\n# 次の表\n[agents.claude]\ncommand = \"claude\"\n",
			ops:  []tomledit.Op{op("/e", "agents", "omp", "exit_command")},
			want: "listen_addr = \"127.0.0.1:1\"\n[agents.omp]\ncommand = \"omp\" # c\nexit_command = \"/e\"\n\n# 次の表\n[agents.claude]\ncommand = \"claude\"\n",
		},
		{
			name: "キーが 1 つも無い表はヘッダーの次の行に足す",
			src:  "listen_addr = \"127.0.0.1:1\"\n[agents.omp] # c\n[agents.claude]\n",
			ops:  []tomledit.Op{op("omp", "agents", "omp", "command")},
			want: "listen_addr = \"127.0.0.1:1\"\n[agents.omp] # c\ncommand = \"omp\"\n[agents.claude]\n",
		},
		{
			name: "末尾の表に足す（改行なしで終わるファイル）",
			src:  "listen_addr = \"127.0.0.1:1\"\n[agents.omp]\ncommand = \"omp\"",
			ops:  []tomledit.Op{op("/e", "agents", "omp", "exit_command")},
			want: "listen_addr = \"127.0.0.1:1\"\n[agents.omp]\ncommand = \"omp\"\nexit_command = \"/e\"",
		},
		{
			name: "無い表は末尾に空行 1 つ・ヘッダー・キーを足す",
			src:  "listen_addr = \"127.0.0.1:1\"\n",
			ops:  []tomledit.Op{op("omp", "agents", "omp", "command"), op("/e", "agents", "omp", "exit_command"), op("c", "agents", "claude", "command")},
			want: "listen_addr = \"127.0.0.1:1\"\n\n[agents.omp]\ncommand = \"omp\"\nexit_command = \"/e\"\n\n[agents.claude]\ncommand = \"c\"\n",
		},
		{
			name: "末尾が既に空行なら空行は増やさない",
			src:  "listen_addr = \"127.0.0.1:1\"\n\n",
			ops:  []tomledit.Op{op("omp", "agents", "omp", "command")},
			want: "listen_addr = \"127.0.0.1:1\"\n\n[agents.omp]\ncommand = \"omp\"\n",
		},
		{
			name: "最上位のキーと無い表を同時に足す",
			src:  "listen_addr = \"127.0.0.1:1\"",
			ops:  []tomledit.Op{op("omp", "agents", "omp", "command"), op([]string{"x"}, "sessions")},
			want: "listen_addr = \"127.0.0.1:1\"\nsessions = [\"x\"]\n\n[agents.omp]\ncommand = \"omp\"",
		},
		{
			name:   "別のキー・未知のキー・表は触らない",
			src:    "listen_addr = \"127.0.0.1:1\"\nmystery = 1\n[pane_names]\n\"0:1.2\" = \"build\"\n[unknown.table]\nz = [1, 2]\n",
			ops:    []tomledit.Op{op([]string{"a"}, "sessions")},
			want:   "listen_addr = \"127.0.0.1:1\"\nmystery = 1\nsessions = [\"a\"]\n[pane_names]\n\"0:1.2\" = \"build\"\n[unknown.table]\nz = [1, 2]\n",
			noLoad: true,
		},
		{
			name: "文字列は基本文字列でエスケープする",
			src:  "listen_addr = \"127.0.0.1:1\"\ntitle_prefix = \"x\"\n",
			ops:  []tomledit.Op{op("a\"b\\c\td é", "title_prefix")},
			want: "listen_addr = \"127.0.0.1:1\"\ntitle_prefix = \"a\\\"b\\\\c\\td é\"\n",
			// タブは serve が title_prefix で拒否する（制御文字）。エスケープの確認だけ。
			noLoad: true,
		},
		{
			name: "操作なしならそのまま",
			src:  base,
			want: base,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tomledit.Edit([]byte(tt.src), tt.ops)
			if err != nil {
				t.Fatalf("Edit: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("結果が違います\n got: %q\nwant: %q", got, tt.want)
			}
			if !tt.noLoad {
				if _, err := config.ParseBytes(got, "config.toml"); err != nil {
					t.Fatalf("結果が serve と同じ読み込みで落ちます: %v", err)
				}
			}
		})
	}
}

func TestEditCommentedArray(t *testing.T) {
	src := "listen_addr = \"127.0.0.1:1\"\nallowed_roots = [\n  \"/a\", # 大事\n  \"/b\",\n]\nsessions = [\"0\"] # 1 行ならコメントは外\n"
	keys := [][]string{{"allowed_roots"}, {"sessions"}, {"nope"}}
	got, err := tomledit.CommentedArrays([]byte(src), keys)
	if err != nil || len(got) != 1 || got[0][0] != "allowed_roots" {
		t.Fatalf("CommentedArrays = %v, %v", got, err)
	}
	_, err = tomledit.Edit([]byte(src), []tomledit.Op{op([]string{"/c"}, "allowed_roots")})
	var cl *tomledit.CommentLossError
	if !errors.As(err, &cl) {
		t.Fatalf("コメント入りの配列は了承なしでは置き換えません: %v", err)
	}
	out, err := tomledit.Edit([]byte(src), []tomledit.Op{{Key: []string{"allowed_roots"}, Value: []string{"/c"}, AllowCommentLoss: true}, op([]string{"x"}, "sessions")})
	if err != nil {
		t.Fatal(err)
	}
	want := "listen_addr = \"127.0.0.1:1\"\nallowed_roots = [\"/c\"]\nsessions = [\"x\"] # 1 行ならコメントは外\n"
	if string(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// 挿入先を決められない形は、何も書かずに中止する（一部だけ書かない）。
func TestEditPlacementErrors(t *testing.T) {
	tests := []struct{ name, src string }{
		{"インラインテーブルへの追加", "listen_addr = \"127.0.0.1:1\"\nagents = { omp = { command = \"omp\" } }\n"},
		{"インラインテーブルの表への追加", "listen_addr = \"127.0.0.1:1\"\nagents.omp = { command = \"omp\" }\n"},
		{"同じ表が離れて 2 回", "[agents.omp]\ncommand = \"a\"\n[x]\n[agents.omp]\n"},
		{"dotted key で別の表に分かれている", "listen_addr = \"127.0.0.1:1\"\nagents.omp.command = \"omp\"\n"},
		{"[[...]] の中", "[[agents]]\nname = \"x\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tomledit.Edit([]byte(tt.src), []tomledit.Op{op("/e", "agents", "omp", "exit_command")})
			var pe *tomledit.PlacementError
			if !errors.As(err, &pe) {
				t.Fatalf("PlacementError になるはず: %v", err)
			}
		})
	}
}

func TestEditRejects(t *testing.T) {
	tests := []struct {
		name, src string
		ops       []tomledit.Op
	}{
		{"TOML として読めない", "listen_addr = \n", []tomledit.Op{op("x", "sessions")}},
		{"同じキーを 2 回書く操作", "a = 1\n", []tomledit.Op{op("x", "sessions"), op("y", "sessions")}},
		{"空のキー", "a = 1\n", []tomledit.Op{{Key: nil, Value: "x"}}},
		{"書けない型", "a = 1\n", []tomledit.Op{op(1.5, "sessions")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out, err := tomledit.Edit([]byte(tt.src), tt.ops); err == nil {
				t.Fatalf("エラーになるはず: %q", out)
			}
		})
	}
}

func TestEditDuplicateKeyInFile(t *testing.T) {
	_, err := tomledit.Edit([]byte("a = 1\na = 2\n"), []tomledit.Op{op("x", "a")})
	var pe *tomledit.PlacementError
	if !errors.As(err, &pe) {
		t.Fatalf("PlacementError になるはず: %v", err)
	}
}
