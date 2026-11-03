package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// file_get_roots・file_get_max_bytes は、書かなければ「追加の根なし・20 MiB」、書いたら書いた値（~ 展開・シンボリックリンク解決・重複除去）が効く。
func TestFileGetConfig(t *testing.T) {
	cfg, _, err := writeConfig(t, minimal)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.FileGetRoots) != 0 || cfg.FileGetMaxBytes != 20*1024*1024 {
		t.Fatalf("既定 = %v・%d", cfg.FileGetRoots, cfg.FileGetMaxBytes)
	}

	shots := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(shots, link); err != nil {
		t.Fatal(err)
	}
	body := minimal + "file_get_roots = [\"" + link + "\", \"" + shots + "/\", \"~/shots\"]\nfile_get_max_bytes = 1024\n"
	cfg, dir, err := writeConfig(t, body)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{resolvePath(shots), filepath.Join(dir, "home", "shots")}
	if !reflect.DeepEqual(cfg.FileGetRoots, want) || cfg.FileGetMaxBytes != 1024 {
		t.Fatalf("file_get_roots = %v（want %v）・file_get_max_bytes = %d", cfg.FileGetRoots, want, cfg.FileGetMaxBytes)
	}
}

func TestFileGetConfigRejectsBadValues(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"file_get_roots 相対", minimal + "file_get_roots = [\"shots\"]\n", "絶対パス"},
		{"file_get_roots に空文字", minimal + "file_get_roots = [\"\"]\n", "空のパス"},
		{"file_get_roots に ~user", minimal + "file_get_roots = [\"~root/x\"]\n", "~/ の形"},
		{"file_get_max_bytes が 0", minimal + "file_get_max_bytes = 0\n", "file_get_max_bytes は 1 以上"},
		{"file_get_max_bytes が負", minimal + "file_get_max_bytes = -1\n", "file_get_max_bytes は 1 以上"},
		{"file_get_max_bytes が上限超え", minimal + "file_get_max_bytes = 104857601\n", "file_get_max_bytes は 1 以上"},
		{"file_get_max_bytes が文字列", minimal + "file_get_max_bytes = \"20MB\"\n", "TOML"},
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
