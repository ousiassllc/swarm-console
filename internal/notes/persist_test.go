package notes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
)

// crashBeforeRename は一時ファイルに途中まで書いたところで落ちた状態（rename 前）を再現する。
func crashBeforeRename(path string, data []byte, _ os.FileMode) error {
	tmp := path + ".tmp-crash"
	if err := os.WriteFile(tmp, data[:len(data)/2], 0o600); err != nil {
		return err
	}
	return errors.New("injected crash")
}

func TestCrashDuringWriteKeepsOldVersion(t *testing.T) {
	e := newEnv(t)
	e.set("%1", map[string]any{"from": "旧"})
	old, _ := os.ReadFile(e.path)

	crashing := e.open(WithWriteFile(crashBeforeRename))
	_, err := crashing.Set(context.Background(), "%1", setOf(map[string]any{"from": "新"}))
	wantCode(t, err, apperr.Internal, "")
	if n, _ := crashing.Get("%1"); n.From != "旧" {
		t.Fatalf("書けなかったのにメモリが進んだ: %+v", n)
	}
	if _, err := crashing.Log(context.Background(), "%1", "x"); err == nil {
		t.Fatal("Log も失敗するはず")
	}
	if ok, err := crashing.Archive(context.Background(), ArchiveMeta{PaneID: "%1"}, ClosedByPaneClose); err == nil || ok {
		t.Fatal("Archive も失敗するはず")
	}
	if _, err := os.Stat(e.path + ".tmp-crash"); err != nil {
		t.Fatalf("一時ファイルだけが残った状態になっていない: %v", err)
	}
	after, _ := os.ReadFile(e.path)
	if string(after) != string(old) {
		t.Fatal("本体が変わった")
	}
	reopened := e.open()
	n, ok := reopened.Get("%1")
	if !ok || n.From != "旧" {
		t.Fatalf("読み直して旧版でない: %+v", n)
	}
	if _, err := os.Stat(e.path + ".tmp-crash"); err != nil {
		t.Fatal("一時ファイルを読み込みが消した（壊れ扱いにしてはいけない）")
	}
	if matches, _ := filepath.Glob(e.path + ".corrupt-*"); len(matches) != 0 {
		t.Fatal("正常なファイルを退避した")
	}
}

func TestCorruptFileQuarantined(t *testing.T) {
	cases := map[string]string{
		"truncated":     `{"version":1,"panes":{"%1":{"from":`,
		"garbage":       "\x00\x01 not json",
		"empty":         "",
		"wrong version": `{"version":99,"panes":{}}`,
		"null note":     `{"version":1,"panes":{"%1":null}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(e.path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			s := e.open()
			if len(s.ActiveIDs()) != 0 {
				t.Fatal("空で開始していない")
			}
			matches, _ := filepath.Glob(e.path + ".corrupt-*")
			if len(matches) != 1 || !strings.Contains(matches[0], "20261031T221005") {
				t.Fatalf("退避先: %v", matches)
			}
			if b, _ := os.ReadFile(matches[0]); string(b) != content {
				t.Fatal("退避したファイルの中身が元と違う")
			}
			e.s = s
			e.set("%1", map[string]any{"from": "新規"})
			if n, ok := e.open().Get("%1"); !ok || n.From != "新規" {
				t.Fatal("退避後に書けない")
			}
		})
	}
}

func TestQuarantineNameCollision(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(e.path, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		e.open()
	}
	if matches, _ := filepath.Glob(e.path + ".corrupt-*"); len(matches) != 2 {
		t.Fatalf("同時刻の退避が上書きされた: %v", matches)
	}
}

func TestFilePermissions(t *testing.T) {
	e := newEnv(t)
	e.set("%1", map[string]any{"from": "x"})
	fi, err := os.Stat(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file = %o", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(e.path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir = %o", di.Mode().Perm())
	}
}

func TestNewStoreRequiresDetector(t *testing.T) {
	e := newEnv(t)
	if _, err := NewStore(e.path, e.clk); err == nil {
		t.Fatal("検出器なしで開けてはいけない")
	}
}

func TestFileLayoutAndKeyOrder(t *testing.T) {
	e := newEnv(t)
	if _, err := e.s.ReconcileServer(context.Background(), "1761896400"); err != nil {
		t.Fatal(err)
	}
	e.set("%12", map[string]any{"memo": "m", "from": "agent-b", "status": "CI 待ち"})
	raw, _ := os.ReadFile(e.path)
	s := string(raw)
	order := []string{`"version"`, `"tmux_server_start"`, `"panes"`, `"from"`, `"status"`, `"memo"`, `"updated_at"`, `"log"`, `"history"`}
	pos := -1
	for _, k := range order {
		i := strings.Index(s, k)
		if i <= pos {
			t.Fatalf("キー順が宣言順でない（%s）:\n%s", k, s)
		}
		pos = i
	}
}

// 退避の警告は WithLogger のロガーへ出る（NFR-OPS-5）。
func TestCorruptFileWarningUsesInjectedLogger(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path, []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	e.open(WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("警告が 1 行の JSON でない: %q: %v", buf.String(), err)
	}
	if rec["level"] != "WARN" || rec["msg"] != "notes.json が壊れているため退避して空で開始します" ||
		rec["path"] != e.path || rec["moved_to"] == nil || rec["reason"] == nil {
		t.Errorf("警告の中身が違う: %v", rec)
	}
}
