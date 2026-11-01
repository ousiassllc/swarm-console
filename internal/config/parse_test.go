package config

import (
	"errors"
	"strings"
	"testing"
)

// FormatError は構文エラーと未知のキーだけ。値の検証エラー・型の違いは含まない（setup が開いて直させる。FR-SET-8）。
func TestParseBytesFormatError(t *testing.T) {
	tests := []struct {
		name, src string
		format    bool
		want      string
	}{
		{"構文エラー", "listen_addr = \n", true, "1 行"},
		{"未知のキー", "listen_addr = \"127.0.0.1:1\"\nmystery = 1\n", true, "mystery（2 行目）"},
		{"未知のキー（表の中）", "listen_addr = \"127.0.0.1:1\"\n[agents.omp]\nnope = 1\n", true, "agents.omp.nope（3 行目）"},
		{"listen_addr 未設定", "sessions = [\"0\"]\n", false, "listen_addr"},
		{"値の検証エラー", "listen_addr = \"0.0.0.0:1\"\n", false, "0.0.0.0"},
		{"型が違う", "listen_addr = \"127.0.0.1:1\"\nsessions = \"a\"\n", false, "2 行"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse([]byte(tt.src), "/x/config.toml", "/home/u")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q を含む", err, tt.want)
			}
			var fe *FormatError
			if got := errors.As(err, &fe); got != tt.format {
				t.Fatalf("FormatError = %v, want %v（%v）", got, tt.format, err)
			}
		})
	}
}

func TestParseBytesMatchesLoad(t *testing.T) {
	src := "listen_addr = \"127.0.0.1:1\"\nsessions = [\"a\", \"b\"]\ndefault_session = \"b\"\n"
	cfg, err := parse([]byte(src), "/x/config.toml", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultSession != "b" || cfg.ConfigDir != "/x" || cfg.DeniedPaths[0] != "/home/u/dotfiles" {
		t.Fatalf("cfg = %+v", cfg)
	}
}
