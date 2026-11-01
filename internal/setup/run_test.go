package setup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

// fakePrompter は画面の代わりに、決めた答えを返す（画面そのものは実 tmux のテストで確かめる）。
type fakePrompter struct {
	ask       func(st *State) (Answers, error)
	loss      bool
	lossAsked []string
}

func (f *fakePrompter) Ask(_ context.Context, st *State) (Answers, error) { return f.ask(st) }

func (f *fakePrompter) ConfirmCommentLoss(_ context.Context, key string) (bool, error) {
	f.lossAsked = append(f.lossAsked, key)
	return f.loss, nil
}

type issued struct {
	name  string
	scope auth.Scope
	err   error
	n     int
}

func (i *issued) issue(name string, scope auth.Scope) error {
	i.n++
	i.name, i.scope = name, scope
	return i.err
}

func run(t *testing.T, path string, p Prompter, tok *issued) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), Options{Path: path, Out: &out, Home: testHome, Prompter: p, IssueToken: tok.issue})
	return out.String(), err
}

func answer(edit func(st *State, v *Values)) func(st *State) (Answers, error) {
	return func(st *State) (Answers, error) {
		v := answersOf(st)
		edit(st, &v)
		return Answers{Values: v}, nil
	}
}

func TestRunNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swarm", "config.toml")
	p := &fakePrompter{ask: answer(func(_ *State, v *Values) { v.ListenAddr = "127.0.0.1:48120" })}
	out, err := run(t, path, p, &issued{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"保存しました: " + path, "- listen_addr", RestartHint} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q がありません:\n%s", want, out)
		}
	}
	b, err := os.ReadFile(path) //nolint:gosec // テストが作った一時ファイル
	if err != nil || !strings.HasSuffix(string(b), "listen_addr = \"127.0.0.1:48120\"\n") {
		t.Fatalf("config.toml = %q, %v", b, err)
	}
	if strings.Contains(out, "denied_paths") {
		t.Fatal("触っていない denied_paths は変更したキーに出ない")
	}
}

// 中止（Ctrl+C・Esc）では何も書かない。
func TestRunAbortWritesNothing(t *testing.T) {
	p := &fakePrompter{ask: func(*State) (Answers, error) { return Answers{}, ErrAborted }}
	t.Run("新規", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if _, err := run(t, path, p, &issued{}); !IsAbort(err) {
			t.Fatalf("err = %v", err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("何か作られました: %v", entries)
		}
	})
	t.Run("既存", func(t *testing.T) {
		src := handwritten(t)
		path := writeConfig(t, src)
		if _, err := run(t, path, p, &issued{}); !IsAbort(err) {
			t.Fatalf("err = %v", err)
		}
		if b := readFile(t, path); string(b) != src {
			t.Fatal("中止なのに書き換えました")
		}
	})
}

// 構文エラー・未知のキーは画面を出さずに終わる（ファイルはそのまま）。
func TestRunRefusesBrokenFileWithoutPrompting(t *testing.T) {
	const src = "listen_addr = \"127.0.0.1:1\"\nmystery = 1\n"
	path := writeConfig(t, src)
	p := &fakePrompter{ask: func(*State) (Answers, error) { t.Fatal("画面を出してはいけない"); return Answers{}, nil }}
	_, err := run(t, path, p, &issued{})
	if err == nil || !strings.Contains(err.Error(), "mystery（2 行目）") {
		t.Fatalf("err = %v", err)
	}
	if b := readFile(t, path); string(b) != src {
		t.Fatal("ファイルが変わりました")
	}
}

// 値が不正（listen_addr 未設定）なファイルは開いて直させる。直した結果だけ書く。
func TestRunFixesInvalidFile(t *testing.T) {
	const src = "# 手書き\nsessions = [\"0\"]\n"
	path := writeConfig(t, src)
	var sawInvalid bool
	p := &fakePrompter{ask: func(st *State) (Answers, error) {
		sawInvalid = st.Invalid != nil
		v := answersOf(st)
		v.ListenAddr = "127.0.0.1:48120"
		return Answers{Values: v}, nil
	}}
	if _, err := run(t, path, p, &issued{}); err != nil || !sawInvalid {
		t.Fatalf("err=%v sawInvalid=%v", err, sawInvalid)
	}
	b := readFile(t, path)
	if want := src + "listen_addr = \"127.0.0.1:48120\"\n"; string(b) != want {
		t.Fatalf("got %q want %q", b, want)
	}
}

// 保存前の検査で落ちたら何も書かない。
func TestRunRefusesInvalidResult(t *testing.T) {
	const src = "listen_addr = \"127.0.0.1:1\"\n"
	path := writeConfig(t, src)
	p := &fakePrompter{ask: answer(func(_ *State, v *Values) { v.ListenAddr = "0.0.0.0:48120" })}
	_, err := run(t, path, p, &issued{})
	if err == nil || !strings.Contains(err.Error(), "何も書きません") {
		t.Fatalf("err = %v", err)
	}
	if b := readFile(t, path); string(b) != src {
		t.Fatal("書き換えました")
	}
}

// 画面を開いている間に別の人が書き換えたら、上書きしない。
func TestRunConflictDuringPrompt(t *testing.T) {
	path := writeConfig(t, "listen_addr = \"127.0.0.1:1\"\n")
	const other = "listen_addr = \"127.0.0.1:9\" # 別の人\n"
	p := &fakePrompter{ask: func(st *State) (Answers, error) {
		if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
			t.Fatal(err)
		}
		v := answersOf(st)
		v.ListenAddr = "127.0.0.1:2"
		return Answers{Values: v}, nil
	}}
	if _, err := run(t, path, p, &issued{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if b := readFile(t, path); string(b) != other {
		t.Fatal("上書きしました")
	}
}

// 変更が無ければ書かない（ファイルの更新時刻・中身とも同じ）。
func TestRunNoChange(t *testing.T) {
	src := handwritten(t)
	path := writeConfig(t, src)
	before, _ := os.Stat(path)
	p := &fakePrompter{ask: answer(func(*State, *Values) {})}
	out, err := run(t, path, p, &issued{})
	if err != nil || !strings.Contains(out, "変更はありません") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("書き換えました")
	}
}

func TestRunCommentLossConsent(t *testing.T) {
	edit := answer(func(_ *State, v *Values) { v.AllowedRoots = []string{"/mnt/nvme/"} })
	t.Run("了承しない: そのキーは元のまま", func(t *testing.T) {
		src := handwritten(t)
		path := writeConfig(t, src)
		p := &fakePrompter{ask: edit}
		out, err := run(t, path, p, &issued{})
		if err != nil || len(p.lossAsked) != 1 || p.lossAsked[0] != "allowed_roots" || !strings.Contains(out, "変更はありません") {
			t.Fatalf("asked=%v out=%q err=%v", p.lossAsked, out, err)
		}
		if b := readFile(t, path); string(b) != src {
			t.Fatal("了承なしで書き換えました")
		}
	})
	t.Run("了承する: コメントごと 1 行にする", func(t *testing.T) {
		path := writeConfig(t, handwritten(t))
		if _, err := run(t, path, &fakePrompter{ask: edit, loss: true}, &issued{}); err != nil {
			t.Fatal(err)
		}
		if b := readFile(t, path); !strings.Contains(string(b), "allowed_roots = [\"/mnt/nvme/\"]\n") {
			t.Fatalf("%s", b)
		}
	})
}

// トークンは保存のあとに発行する。発行に失敗しても保存済みの config.toml は変えない。
func TestRunTokenAfterSave(t *testing.T) {
	ask := func(st *State) (Answers, error) {
		v := answersOf(st)
		v.ListenAddr = "127.0.0.1:48120"
		return Answers{Values: v, Token: &TokenRequest{Name: "dev", Scope: auth.ScopeOps}}, nil
	}
	t.Run("成功", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		tok := &issued{}
		if _, err := run(t, path, &fakePrompter{ask: ask}, tok); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil || tok.n != 1 || tok.name != "dev" || tok.scope != auth.ScopeOps {
			t.Fatalf("tok=%+v stat=%v", tok, err)
		}
	})
	t.Run("失敗", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		tok := &issued{err: errors.New("名前 dev は既に使われています")}
		_, err := run(t, path, &fakePrompter{ask: ask}, tok)
		if err == nil || !strings.Contains(err.Error(), "保存した config.toml はそのまま") {
			t.Fatalf("err = %v", err)
		}
		if b := readFile(t, path); !strings.Contains(string(b), "listen_addr = \"127.0.0.1:48120\"") {
			t.Fatal("保存した内容が失われました")
		}
	})
	t.Run("保存に失敗したら発行しない", func(t *testing.T) {
		path := writeConfig(t, "listen_addr = \"127.0.0.1:1\"\n")
		tok := &issued{}
		p := &fakePrompter{ask: func(st *State) (Answers, error) {
			if err := os.WriteFile(path, []byte("listen_addr = \"127.0.0.1:9\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return ask(st)
		}}
		if _, err := run(t, path, p, tok); !errors.Is(err, ErrConflict) || tok.n != 0 {
			t.Fatalf("err=%v issued=%d", err, tok.n)
		}
	})
}
