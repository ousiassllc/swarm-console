package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeConfig は dir/config.toml に body を書いて load する（home は dir/home）。
func writeConfig(t *testing.T, body string) (*Config, string, error) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "conf", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := load(path, home)
	return cfg, dir, err
}

const minimal = `listen_addr = "100.64.1.2:48120"` + "\n"

func TestLoadDefaults(t *testing.T) {
	cfg, dir, err := writeConfig(t, minimal)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	conf := filepath.Join(dir, "conf")
	want := &Config{
		ListenAddr:       "100.64.1.2:48120",
		Sessions:         []string{"0"},
		DefaultSession:   "0",
		DefaultLang:      "ja",
		PollInterval:     5 * time.Second,
		SnapshotTTL:      2 * time.Second,
		StalledAfter:     15 * time.Minute,
		LayoutWarningTTL: 10 * time.Minute,
		AcceptTimeout:    10 * time.Second,
		StartTimeout:     30 * time.Second,
		LockTimeout:      60 * time.Second,
		TitlePrefix:      "kana: ",
		DeniedPaths:      []string{filepath.Join(home, "dotfiles")},
		DiskMounts:       []string{"/", "/mnt/nvme"},
		ReportDir:        "/tmp/swarm-console/reports",
		ConfigDir:        conf,
		TokensPath:       filepath.Join(conf, "tokens.toml"),
		TemplatesDir:     filepath.Join(conf, "templates"),
		ProfilesPath:     filepath.Join(conf, "profiles.toml"),
		NotesPath:        filepath.Join(home, ".local", "share", "swarm-console", "notes.json"),
	}
	// allowed_roots・file_roots はシンボリックリンク解決後（この機の /mnt/nvme の実体）になりうるので別に確かめる。
	if want.AllowedRoots = cfg.AllowedRoots; len(cfg.AllowedRoots) != 2 {
		t.Fatalf("allowed_roots = %v", cfg.AllowedRoots)
	}
	if want.FileRoots = cfg.FileRoots; len(cfg.FileRoots) != 3 || cfg.FileRoots[0] != resolvePath("/tmp") || !reflect.DeepEqual(cfg.FileRoots[1:], cfg.AllowedRoots) {
		t.Fatalf("file_roots = %v（/tmp ＋ allowed_roots のはず）", cfg.FileRoots)
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", cfg, want)
	}
}

func TestLoadFull(t *testing.T) {
	root := t.TempDir()
	body := `
listen_addr = "[fd7a:115c:a1e0::1]:48120"
tmux_socket = "sc-test"
sessions = ["work", "0"]
default_session = "work"
default_lang = "en"
poll_interval = "1s"
snapshot_ttl = "100ms"
stalled_after = "1h"
layout_warning_ttl = "1m"
accept_timeout = "5s"
start_timeout = "1m"
lock_timeout = "2m"
title_prefix = ""
allowed_roots = ["` + root + `/", "~/work"]
denied_paths = ["` + root + `/co//../co", "~/dotfiles"]
file_roots = ["/tmp/x/../"]
disk_mounts = ["/var/"]
report_dir = "~/reports"

[pane_names]
"work:0.1" = "main"
"0:1.0" = "Docs_2"

[agents.claude]
command = "claude --x"
process_match = ['(^|/)claude$', "node.*claude"]
ask_re = '^\s*❯ \d+\.'
auth_file = "~/.claude/cred.json"

[agents.omp]
exit_command = "/exit"
`
	cfg, dir, err := writeConfig(t, body)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	checks := []struct {
		name      string
		got, want any
	}{
		{"ListenAddr", cfg.ListenAddr, "[fd7a:115c:a1e0::1]:48120"},
		{"TmuxSocket", cfg.TmuxSocket, "sc-test"},
		{"Sessions", cfg.Sessions, []string{"work", "0"}},
		{"DefaultSession", cfg.DefaultSession, "work"},
		{"DefaultLang", cfg.DefaultLang, "en"},
		{"PollInterval", cfg.PollInterval, time.Second},
		{"SnapshotTTL", cfg.SnapshotTTL, 100 * time.Millisecond},
		{"StalledAfter", cfg.StalledAfter, time.Hour},
		{"TitlePrefix", cfg.TitlePrefix, ""},
		{"AllowedRoots", cfg.AllowedRoots, []string{resolvePath(root), filepath.Join(home, "work")}},
		{"DeniedPaths", cfg.DeniedPaths, []string{filepath.Join(home, "dotfiles"), resolvePath(root) + "/co"}},
		{"FileRoots", cfg.FileRoots, []string{"/tmp"}},
		{"DiskMounts", cfg.DiskMounts, []string{"/var"}},
		{"ReportDir", cfg.ReportDir, filepath.Join(home, "reports")},
		{"PaneNames", cfg.PaneNames, map[string]string{"work:0.1": "main", "0:1.0": "Docs_2"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	cl := cfg.Agents["claude"]
	if cl.Command == nil || *cl.Command != "claude --x" || len(cl.ProcessMatch) != 2 || cl.AskRE == nil ||
		cl.AuthFile == nil || *cl.AuthFile != filepath.Join(home, ".claude", "cred.json") || cl.WorkingRE != nil {
		t.Errorf("agents.claude = %+v", cl)
	}
	if o := cfg.Agents["omp"]; o.ExitCommand == nil || *o.ExitCommand != "/exit" {
		t.Errorf("agents.omp = %+v", o)
	}
}

func TestLoadSymlinkResolvedOnlyWhenExists(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	body := minimal + `allowed_roots = ["` + root + `/link", "` + root + `/missing/../absent"]` + "\n"
	cfg, _, err := writeConfig(t, body)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllowedRoots[0] != resolvePath(target) {
		t.Errorf("リンクは解決される: %v", cfg.AllowedRoots)
	}
	if cfg.AllowedRoots[1] != filepath.Join(root, "absent") {
		t.Errorf("存在しないパスは Clean だけ: %v", cfg.AllowedRoots)
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"未知キー（打ち間違い）", minimal + "pol_interval = \"5s\"\n", "未知のキー"},
		{"未知キー（agents 内）", minimal + "[agents.omp]\nworking = \"x\"\n", "未知のキー"},
		{"未知キー（ネスト）", minimal + "[extra]\na = 1\n", "未知のキー"},
		{"TOML が壊れている", "listen_addr = \n", "TOML"},
		{"listen_addr なし", "tmux_socket = \"x\"\n", "listen_addr は必須"},
		{"sessions 空", minimal + "sessions = []\n", "sessions は 1 つ以上"},
		{"sessions 重複", minimal + "sessions = [\"a\", \"a\"]\n", "重複"},
		{"sessions に ':'", minimal + "sessions = [\"a:b\"]\n", "使えない名前"},
		{"default_session が範囲外", minimal + "default_session = \"x\"\n", "default_session"},
		{"default_lang 不正", minimal + "default_lang = \"fr\"\n", "default_lang"},
		{"tmux_socket にスラッシュ", minimal + "tmux_socket = \"a/b\"\n", "tmux_socket"},
		{"tmux_socket が先頭 -", minimal + "tmux_socket = \"-x\"\n", "tmux_socket"},
		{"期間が文字列でない", minimal + "poll_interval = 5\n", "TOML"},
		{"期間の形式が不正", minimal + "poll_interval = \"five\"\n", "poll_interval"},
		{"poll_interval 下限", minimal + "poll_interval = \"999ms\"\n", "poll_interval は 1s 以上"},
		{"snapshot_ttl 下限", minimal + "snapshot_ttl = \"99ms\"\n", "snapshot_ttl は 100ms 以上"},
		{"stalled_after が 0", minimal + "stalled_after = \"0s\"\n", "stalled_after は 0 より大きい"},
		{"lock_timeout が負", minimal + "lock_timeout = \"-1s\"\n", "lock_timeout は 0 より大きい"},
		{"title_prefix に改行", minimal + "title_prefix = \"a\\nb\"\n", "title_prefix"},
		{"allowed_roots 相対", minimal + "allowed_roots = [\"work\"]\n", "絶対パス"},
		{"allowed_roots 空", minimal + "allowed_roots = []\n", "allowed_roots は 1 つ以上"},
		{"allowed_roots に空文字", minimal + "allowed_roots = [\"\"]\n", "空のパス"},
		{"denied_paths に ~user", minimal + "denied_paths = [\"~root/x\"]\n", "~/ の形"},
		{"file_roots に制御文字", minimal + "file_roots = [\"/tmp/a\\u0001\"]\n", "制御文字"},
		{"report_dir 相対", minimal + "report_dir = \"reports\"\n", "絶対パス"},
		{"pane_names のキー形式", minimal + "[pane_names]\nfoo = \"x\"\n", "pane_names のキー"},
		{"pane_names のセッション外", minimal + "[pane_names]\n\"zz:0.1\" = \"x\"\n", "pane_names のキー"},
		{"agents.<name> 未対応", minimal + "[agents.gpt]\ncommand = \"x\"\n", "未対応"},
		{"agents 正規表現不正", minimal + "[agents.omp]\nworking_re = \"(\"\n", "agents.omp.working_re"},
		{"agents process_match 不正", minimal + "[agents.codex]\nprocess_match = [\"ok\", \"[\"]\n", "process_match[1]"},
		{"agents command 改行", minimal + "[agents.omp]\ncommand = \"omp\\nrm\"\n", "agents.omp.command"},
		{"agents command 空", minimal + "[agents.omp]\ncommand = \"\"\n", "空にできません"},
		{"agents auth_file 相対", minimal + "[agents.omp]\nauth_file = \"a.json\"\n", "絶対パス"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _, err := writeConfig(t, tt.body)
			if err == nil {
				t.Fatalf("エラーのはず: %+v", cfg)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, %q を含むはず", err, tt.want)
			}
		})
	}
}

func TestLoadMissingFileAndUnknownKeyPosition(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); !os.IsNotExist(unwrapAll(err)) {
		t.Fatalf("存在しないファイル: %v", err)
	}
	_, _, err := writeConfig(t, minimal+"\nbad_key = 1\n")
	if err == nil || !strings.Contains(err.Error(), "bad_key（3 行目）") {
		t.Fatalf("キーと行番号が出るはず: %v", err)
	}
}

func unwrapAll(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
}

func TestDeniedPathsAlwaysIncludeDotfiles(t *testing.T) {
	cfg, dir, err := writeConfig(t, minimal+"denied_paths = [\"/srv/checkout\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "home", "dotfiles"), "/srv/checkout"}
	if !reflect.DeepEqual(cfg.DeniedPaths, want) {
		t.Fatalf("denied_paths = %v, want %v", cfg.DeniedPaths, want)
	}
}
