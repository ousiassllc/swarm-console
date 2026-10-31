package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/ousiassllc/swarm-console/internal/agent"
)

const maxConfigBytes = 1 << 20

// 既定値（アーキテクチャ設計 §8）。
const (
	defaultPollInterval     = 5 * time.Second
	defaultSnapshotTTL      = 2 * time.Second
	defaultStalledAfter     = 15 * time.Minute
	defaultLayoutWarningTTL = 10 * time.Minute
	defaultAcceptTimeout    = 10 * time.Second
	defaultStartTimeout     = 30 * time.Second
	defaultLockTimeout      = 60 * time.Second
	defaultTitlePrefix      = "kana: "
	defaultReportDir        = "/tmp/swarm-console/reports"

	minPollInterval = time.Second
	minSnapshotTTL  = 100 * time.Millisecond
)

// rawConfig は config.toml のキーそのまま。未指定を区別するため、既定値のあるものはポインタ・nil スライスで持つ。
type rawConfig struct {
	ListenAddr       string                     `toml:"listen_addr"`
	TmuxSocket       string                     `toml:"tmux_socket"`
	Sessions         []string                   `toml:"sessions"`
	DefaultSession   string                     `toml:"default_session"`
	DefaultLang      string                     `toml:"default_lang"`
	PollInterval     *string                    `toml:"poll_interval"`
	SnapshotTTL      *string                    `toml:"snapshot_ttl"`
	StalledAfter     *string                    `toml:"stalled_after"`
	LayoutWarningTTL *string                    `toml:"layout_warning_ttl"`
	AcceptTimeout    *string                    `toml:"accept_timeout"`
	StartTimeout     *string                    `toml:"start_timeout"`
	LockTimeout      *string                    `toml:"lock_timeout"`
	TitlePrefix      *string                    `toml:"title_prefix"`
	AllowedRoots     []string                   `toml:"allowed_roots"`
	DeniedPaths      []string                   `toml:"denied_paths"`
	FileRoots        []string                   `toml:"file_roots"`
	DiskMounts       []string                   `toml:"disk_mounts"`
	ReportDir        string                     `toml:"report_dir"`
	PaneNames        map[string]string          `toml:"pane_names"`
	Agents           map[string]agent.Overrides `toml:"agents"`
}

// Load は config.toml を読み、未知キーの拒否・検証・既定値の適用・`~` 展開・パスの Clean を行う。
// ConfigDir などは path のあるフォルダを基準に決める。エラーは設定の問題箇所（キー名）を含み、値の全文は含めない。
func Load(path string) (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config: ホームフォルダを決められません: %w", err)
	}
	return load(path, home)
}

func load(path, home string) (*Config, error) {
	data, err := readLimited(path)
	if err != nil {
		return nil, fmt.Errorf("config: %s を読めません: %w", path, err)
	}
	var raw rawConfig
	if err := decodeStrict(data, &raw); err != nil {
		return nil, fmt.Errorf("config: %s: %w", filepath.Base(path), err)
	}
	cfg, err := build(&raw, home)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", filepath.Base(path), err)
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("config: 設定フォルダを決められません: %w", err)
	}
	cfg.ConfigDir = dir
	cfg.TokensPath = filepath.Join(dir, "tokens.toml")
	cfg.TemplatesDir = filepath.Join(dir, "templates")
	cfg.ProfilesPath = filepath.Join(dir, "profiles.toml")
	cfg.NotesPath = filepath.Join(home, ".local", "share", "swarm-console", "notes.json")
	return cfg, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // 起動引数で指定された設定ファイル
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, errors.New("ファイルが大きすぎます")
	}
	return data, nil
}

// decodeStrict は未知のキーを拒否して TOML を v に読む。
func decodeStrict(data []byte, v any) error {
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		return nil
	}
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		var keys []string
		for _, e := range strict.Errors {
			row, _ := e.Position()
			keys = append(keys, fmt.Sprintf("%s（%d 行目）", strings.Join(e.Key(), "."), row))
		}
		return fmt.Errorf("未知のキーがあります: %s", strings.Join(keys, ", "))
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		return fmt.Errorf("TOML を読めません（%d 行 %d 桁）: %s", row, col, de.Error())
	}
	return fmt.Errorf("TOML を読めません: %w", err)
}

// build は検証して Config を組み立てる（ConfigDir などは含まない）。
func build(r *rawConfig, home string) (*Config, error) {
	c := &Config{TmuxSocket: r.TmuxSocket}
	var err error
	if c.ListenAddr, err = checkListenAddr(r.ListenAddr); err != nil {
		return nil, err
	}
	if err = checkSocket(r.TmuxSocket); err != nil {
		return nil, err
	}
	if c.Sessions, c.DefaultSession, err = checkSessions(r.Sessions, r.DefaultSession); err != nil {
		return nil, err
	}
	if c.DefaultLang, err = checkLang(r.DefaultLang); err != nil {
		return nil, err
	}
	if err = buildDurations(r, c); err != nil {
		return nil, err
	}
	if err = buildText(r, c); err != nil {
		return nil, err
	}
	if err = buildPaths(r, c, home); err != nil {
		return nil, err
	}
	if c.PaneNames, err = checkPaneNames(r.PaneNames, c.Sessions); err != nil {
		return nil, err
	}
	if c.Agents, err = checkAgents(r.Agents, home); err != nil {
		return nil, err
	}
	return c, nil
}

func checkLang(s string) (string, error) {
	switch s {
	case "":
		return "ja", nil
	case "ja", "en":
		return s, nil
	}
	return "", errors.New(`default_lang は "ja" か "en" です`)
}

func buildText(r *rawConfig, c *Config) error {
	c.TitlePrefix = defaultTitlePrefix
	if r.TitlePrefix != nil {
		if err := checkPlainLine("title_prefix", *r.TitlePrefix); err != nil {
			return err
		}
		c.TitlePrefix = *r.TitlePrefix
	}
	return nil
}
