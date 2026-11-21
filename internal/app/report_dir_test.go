package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareReportDir(t *testing.T) {
	uid := os.Getuid()
	t.Run("新規は 0700 で作る", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "a", "reports")
		if err := prepareReportDir(d, uid); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("fi=%v err=%v", fi, err)
		}
	})
	t.Run("他ユーザー所有は拒否", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "reports")
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := prepareReportDir(d, uid+1); err == nil || !strings.Contains(err.Error(), "持ち主") {
			t.Fatalf("err=%v", err)
		}
	})
}

// report_dir がシンボリックリンクなら New が起動を拒否し、自分所有の 0750 なら 0700 に直して続ける。
func TestNewChecksReportDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	write := func(reportDir string) string {
		cfgPath := filepath.Join(home, "config.toml")
		body := "listen_addr = \"127.0.0.1:0\"\nreport_dir = \"" + reportDir + "\"\nupdate_check = false\nusage_check = false\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return cfgPath
	}
	logger := slog.New(slog.DiscardHandler)

	target := filepath.Join(home, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(write(link), "test", logger); err == nil || !strings.Contains(err.Error(), "シンボリックリンク") {
		t.Fatalf("リンクの report_dir で起動できてしまいます: err=%v", err)
	}

	loose := filepath.Join(home, "loose")
	if err := os.Mkdir(loose, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o750); err != nil { //nolint:gosec // 緩い権限を作るテスト
		t.Fatal(err)
	}
	if _, err := New(write(loose), "test", logger); err != nil {
		t.Fatalf("0750 の report_dir で起動できません: %v", err)
	}
	if fi, err := os.Stat(loose); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("0700 に直っていません: fi=%v err=%v", fi, err)
	}
}
