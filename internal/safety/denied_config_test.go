package safety

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// config.toml の denied_paths は既定（~/dotfiles）と入れ替わる（P-29）。
// 追加したパスの配下は cwd・file とも拒否され、入れ替えで外れた ~/dotfiles は拒否されなくなる。
func TestConfiguredDeniedPathsReplaceDefault(t *testing.T) {
	root := t.TempDir()
	home := mkdir(t, filepath.Join(root, "home"))
	t.Setenv("HOME", home)
	dotfiles := mkdir(t, filepath.Join(home, "dotfiles"))
	checkout := mkdir(t, filepath.Join(root, "work", "checkout"))
	other := mkdir(t, filepath.Join(root, "work", "other"))

	load := func(denied string) *Policy {
		t.Helper()
		body := "listen_addr = \"127.0.0.1:48120\"\n" +
			"allowed_roots = [\"" + root + "\"]\n" +
			"file_roots = [\"" + root + "\"]\n" + denied
		path := filepath.Join(root, "config.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return NewPolicy(cfg)
	}
	dotfile := write(t, filepath.Join(dotfiles, "rc"), "x")
	secret := write(t, filepath.Join(checkout, "note.txt"), "x")
	plain := write(t, filepath.Join(other, "note.txt"), "x")

	def := load("")
	if _, err := def.CheckCWD(dotfiles); code(err) != apperr.ForbiddenPath {
		t.Fatalf("既定: ~/dotfiles の cwd = %v, want forbidden_path", err)
	}
	if _, err := def.CheckFile(dotfile); code(err) != apperr.ForbiddenPath {
		t.Fatalf("既定: ~/dotfiles の file = %v, want forbidden_path", err)
	}

	custom := load("denied_paths = [\"" + checkout + "\"]\n")
	if _, err := custom.CheckCWD(checkout); code(err) != apperr.ForbiddenPath {
		t.Fatalf("追加パスの cwd = %v, want forbidden_path", err)
	}
	if _, err := custom.CheckFile(secret); code(err) != apperr.ForbiddenPath {
		t.Fatalf("追加パス配下の file = %v, want forbidden_path", err)
	}
	if _, err := custom.CheckCWD(other); err != nil {
		t.Fatalf("他の cwd は許可されるはず: %v", err)
	}
	if _, err := custom.CheckFile(plain); err != nil {
		t.Fatalf("他の file は許可されるはず: %v", err)
	}
	if _, err := custom.CheckCWD(dotfiles); err != nil {
		t.Fatalf("入れ替えたので ~/dotfiles は既定の拒否から外れる: %v", err)
	}
}
