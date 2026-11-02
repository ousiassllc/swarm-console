package auth

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/clock"
)

func TestAllowedSessionsParse(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name    string
		extra   string // okRec に足す行
		want    []string
		wantErr string
	}{
		{"キー無しは nil（config の全セッション）", "", nil, ""},
		{"1 つ", "allowed_sessions = [\"0\"]\n", []string{"0"}, ""},
		{"複数", "allowed_sessions = [\"0\", \"work\"]\n", []string{"0", "work"}, ""},
		{"空配列は拒否", "allowed_sessions = []\n", nil, "allowed_sessions"},
		{"重複は拒否", "allowed_sessions = [\"0\", \"0\"]\n", nil, "重複"},
		{"空の名前は拒否", "allowed_sessions = [\"\"]\n", nil, "使えない"},
		{"コロンは拒否", "allowed_sessions = [\"a:b\"]\n", nil, "使えない"},
		{"ドットは拒否", "allowed_sessions = [\"a.b\"]\n", nil, "使えない"},
		{"制御文字は拒否", "allowed_sessions = [\"a\\nb\"]\n", nil, "使えない"},
		{"配列でない値は拒否", "allowed_sessions = \"0\"\n", nil, "TOML"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(c.name, "/", "_")+".toml")
			writeRaw(t, path, okRec+c.extra, 0o600)
			recs, err := readRecords(path)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q を含む", err, c.wantErr)
				}
				if strings.Contains(err.Error(), okHash) {
					t.Fatalf("エラーにハッシュが含まれる: %v", err)
				}
				return
			}
			if err != nil || len(recs) != 1 || !reflect.DeepEqual(recs[0].AllowedSessions, c.want) {
				t.Fatalf("recs = %+v, err = %v", recs, err)
			}
		})
	}
}

func TestAddWithSessionsAndAuthenticate(t *testing.T) {
	f := newFixture(t)
	tok, err := Add(f.path, "kana", ScopeOps, f.clk.Now(), "0")
	if err != nil {
		t.Fatal(err)
	}
	f.tokens["kana"] = tok
	if _, err := Add(f.path, "boss", ScopeOps, f.clk.Now(), "0", "1"); err != nil {
		t.Fatal(err)
	}
	f.open(t)
	c, err := f.a.Authenticate(f.bearer("kana"))
	if err != nil || !reflect.DeepEqual(c.AllowedSessions, []string{"0"}) || c.Name != "kana" || c.Scope != ScopeOps {
		t.Fatalf("Client = %+v, %v", c, err)
	}
	// 許可を指定しないトークンは nil（全セッション）。
	if c, err := f.a.Authenticate(f.bearer("adm")); err != nil || c.AllowedSessions != nil {
		t.Fatalf("省略したトークン: %+v, %v", c, err)
	}
	// 返した Client の変更が内部の状態に響かない。
	c, _ = f.a.Authenticate(f.bearer("kana"))
	c.AllowedSessions[0] = "x"
	if c2, _ := f.a.Authenticate(f.bearer("kana")); c2.AllowedSessions[0] != "0" {
		t.Fatalf("内部の状態が書き換わった: %+v", c2)
	}
	// ファイルにはキーが書かれ、省略したものには書かれない。
	body := readFile(t, f.path)
	if strings.Count(body, "allowed_sessions") != 2 {
		t.Fatalf("allowed_sessions の行数が不正:\n%s", body)
	}
}

func TestAddRejectsBadSessions(t *testing.T) {
	f := newFixture(t)
	before := readFile(t, f.path)
	for name, sessions := range map[string][]string{
		"重複":    {"0", "0"},
		"空の名前":  {""},
		"コロン":   {"a:b"},
		"ドット":   {"a.b"},
		"制御文字":  {"a\tb"},
		"空白だけ":  {"  "},
		"途中に空":  {"0", ""},
		"改行入り":  {"0\n1"},
		"コロン 2": {"1:T1"},
	} {
		if tok, err := Add(f.path, "x", ScopeOps, f.clk.Now(), sessions...); err == nil || tok != "" {
			t.Errorf("%s: 拒否されるはず: tok=%q err=%v", name, tok, err)
		}
	}
	if readFile(t, f.path) != before {
		t.Fatal("拒否したのにファイルが変わった")
	}
}

func openWithLog(t *testing.T, path string) (*Authenticator, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	a, err := Open(path, clock.NewFake(baseTime), slog.New(slog.NewTextHandler(&buf, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return a, &buf
}

// serve の起動時: config の sessions に無い名前を許可するトークンは起動拒否。メッセージはトークン名とセッション名だけ。
func TestCheckSessionsRefusesUnknownSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.toml")
	writeRaw(t, path, okRec+"allowed_sessions = [\"0\", \"9\"]\n", 0o600)
	a, _ := openWithLog(t, path)
	err := a.CheckSessions([]string{"0", "1"})
	if err == nil || !strings.Contains(err.Error(), `"9"`) || !strings.Contains(err.Error(), " a ") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), okHash) {
		t.Fatalf("エラーにハッシュが含まれる: %v", err)
	}
	if err := a.CheckSessions([]string{"0", "9"}); err != nil {
		t.Fatalf("含まれていれば通る: %v", err)
	}
}

// 失効済みのトークンの allowed_sessions は起動を止めない。
func TestCheckSessionsIgnoresRevoked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.toml")
	writeRaw(t, path, okRec+"allowed_sessions = [\"9\"]\nrevoked_at = \"2026-11-01T00:00:00+09:00\"\n", 0o600)
	a, _ := openWithLog(t, path)
	if err := a.CheckSessions([]string{"0"}); err != nil {
		t.Fatalf("失効済みで起動拒否: %v", err)
	}
}

// 起動後の再読み込み（tokens.toml の変更）で config に無い名前が入っても、認証は止めず警告だけ出す。
func TestReloadWithUnknownSessionWarnsButAuthenticates(t *testing.T) {
	f := newFixture(t)
	buf := &bytes.Buffer{}
	a, err := Open(f.path, f.clk, slog.New(slog.NewTextHandler(buf, nil)))
	if err != nil {
		t.Fatal(err)
	}
	f.a = a
	if err := a.CheckSessions([]string{"0"}); err != nil {
		t.Fatal(err)
	}
	tok, err := Add(f.path, "late", ScopeOps, f.clk.Now(), "9")
	if err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, f.path)
	c, err := a.Authenticate("Bearer " + tok)
	if err != nil || !reflect.DeepEqual(c.AllowedSessions, []string{"9"}) {
		t.Fatalf("再読み込み後の認証: %+v, %v", c, err)
	}
	if log := buf.String(); !strings.Contains(log, "late") || !strings.Contains(log, `\"9\"`) || strings.Contains(log, tok) {
		t.Fatalf("警告ログ = %q", log)
	}
}

// allowed_sessions を持たない既存の tokens.toml はそのまま読め、書き戻しでもキーを足さない（後方互換）。
func TestLegacyTokensFileStaysWithoutKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.toml")
	writeRaw(t, path, okRec, 0o600)
	if _, err := Add(path, "next", ScopeReport, baseTime); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // テストが一時フォルダに作ったパス
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "allowed_sessions") {
		t.Fatalf("キーが足されました:\n%s", data)
	}
}
