package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// update_check・update_check_interval・update_auto は書いた値が効く（FR-UPD-10・19）。
func TestLoadUpdateKeys(t *testing.T) {
	cfg, _, err := writeConfig(t, minimal+"update_check = false\nupdate_check_interval = \"90m\"\nupdate_auto = false\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateCheck || cfg.UpdateAuto || cfg.UpdateCheckInterval != 90*time.Minute {
		t.Fatalf("update 設定 = %v / %v / %v", cfg.UpdateCheck, cfg.UpdateCheckInterval, cfg.UpdateAuto)
	}
	// 片方だけ書いたとき、もう片方は既定の true のまま。
	cfg, _, err = writeConfig(t, minimal+"update_auto = false\n")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UpdateCheck || cfg.UpdateAuto || cfg.UpdateCheckInterval != 6*time.Hour {
		t.Fatalf("update_auto だけ false: %v / %v / %v", cfg.UpdateCheck, cfg.UpdateCheckInterval, cfg.UpdateAuto)
	}
}

// 間隔は 1h 以上（ちょうど 1h は通り、59m59s は拒否）。型違いも拒否する。
func TestLoadUpdateRejects(t *testing.T) {
	if cfg, _, err := writeConfig(t, minimal+"update_check_interval = \"1h\"\n"); err != nil || cfg.UpdateCheckInterval != time.Hour {
		t.Fatalf("1h は通るはず: %v", err)
	}
	tests := []struct{ name, body, want string }{
		{"間隔が下限未満", "update_check_interval = \"59m59s\"\n", "update_check_interval は 1h0m0s 以上"},
		{"間隔が 0", "update_check_interval = \"0s\"\n", "update_check_interval は 1h0m0s 以上"},
		{"間隔の形式不正", "update_check_interval = \"daily\"\n", "update_check_interval"},
		{"update_check が文字列", "update_check = \"yes\"\n", "TOML"},
		{"update_auto が数値", "update_auto = 1\n", "TOML"},
		{"update_auto の打ち間違い", "update_autos = true\n", "未知のキー"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _, err := writeConfig(t, minimal+tt.body)
			if err == nil {
				t.Fatalf("エラーのはず: %+v", cfg)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, %q を含むはず", err, tt.want)
			}
		})
	}
}

// setup が保存前に結果を読み直す入口（ParseBytes）も、update_* を含む設定を serve と同じに読み、打ち間違いは FormatError にする（FR-SET-4・5）。
func TestParseBytesAcceptsUpdateKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := ParseBytes([]byte(minimal+"update_auto = false # 手で止めた\nupdate_check_interval = \"6h\"\n"), path)
	if err != nil || cfg.UpdateAuto || !cfg.UpdateCheck || cfg.UpdateCheckInterval != 6*time.Hour {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
	var fe *FormatError
	if _, err := ParseBytes([]byte(minimal+"updte_auto = false\n"), path); !errors.As(err, &fe) {
		t.Fatalf("未知のキーは FormatError のはず: %v", err)
	}
}

// usage_check・usage_interval は書いた値が効く。既定は true・5m、下限は 1m（NFR-OPS-12）。
func TestUsageSettings(t *testing.T) {
	cfg, _, err := writeConfig(t, minimal+"usage_check = false\nusage_interval = \"90s\"\n")
	if err != nil || cfg.UsageCheck || cfg.UsageInterval != 90*time.Second {
		t.Fatalf("書いた値: %v %v %v", cfg, err, cfg.UsageInterval)
	}
	if cfg, _, err = writeConfig(t, minimal+"usage_interval = \"1m\"\n"); err != nil || !cfg.UsageCheck || cfg.UsageInterval != time.Minute {
		t.Fatalf("1m は許す: %v %v", cfg, err)
	}
	for _, tt := range []struct{ name, body, want string }{
		{"下限未満", "usage_interval = \"59s\"\n", "usage_interval は 1m0s 以上"},
		{"形式不正", "usage_interval = \"often\"\n", "usage_interval"},
		{"usage_check が文字列", "usage_check = \"yes\"\n", "TOML"},
	} {
		if _, _, err := writeConfig(t, minimal+tt.body); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v（%q を含むはず）", tt.name, err, tt.want)
		}
	}
}
