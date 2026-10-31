package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadProfilesDefault(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "profiles.toml")} {
		p, err := LoadProfiles(path)
		if err != nil {
			t.Fatalf("%q: %v", path, err)
		}
		if len(p.Common) != 8 || !strings.Contains(p.Common[7], "{report_path}") {
			t.Fatalf("common = %v", p.Common)
		}
		if got := len(p.Profiles); got != 3 {
			t.Fatalf("profiles = %v", p.Profiles)
		}
		if inv := p.Profiles["investigate"]; inv.Name != "investigate" || len(inv.Rules) != 2 {
			t.Fatalf("investigate = %+v", inv)
		}
		if f := p.Profiles["fix"]; len(f.Rules) != 1 || !strings.HasPrefix(f.Rules[0], "PR は 1 本まで。バグの再現確認") {
			t.Fatalf("fix = %+v", f)
		}
		if !strings.HasPrefix(p.Common[0], "コミットは必ず `git commit -s`") {
			t.Fatalf("common[0] = %q", p.Common[0])
		}
	}
}

func profilesBody(common, extra string) string {
	return "[common]\nrules = [" + common + "]\n" +
		"[profile.sweep]\nrules = [\"a\"]\n[profile.fix]\nrules = [\"b\"]\n[profile.investigate]\nrules = [\"c\"]\n" + extra
}

func TestLoadProfilesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.toml")
	body := profilesBody(`"共通 {report_path} と {report_path}"`, "[profile.docs]\nrules = [\"d {report_path}\"]\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Common, []string{"共通 {report_path} と {report_path}"}) || len(p.Profiles) != 4 ||
		!reflect.DeepEqual(p.Profiles["docs"], Profile{Name: "docs", Rules: []string{"d {report_path}"}}) {
		t.Fatalf("%+v", p)
	}
}

func TestLoadProfilesRejects(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"common なし", "[profile.sweep]\nrules=[]\n[profile.fix]\nrules=[]\n[profile.investigate]\nrules=[]\n", "[common]"},
		{"sweep なし", "[common]\nrules=[\"a\"]\n[profile.fix]\nrules=[]\n[profile.investigate]\nrules=[]\n", "profile.sweep"},
		{"fix なし", "[common]\nrules=[\"a\"]\n[profile.sweep]\nrules=[]\n[profile.investigate]\nrules=[]\n", "profile.fix"},
		{"investigate なし", "[common]\nrules=[\"a\"]\n[profile.sweep]\nrules=[]\n[profile.fix]\nrules=[]\n", "profile.investigate"},
		{"空のルール", profilesBody(`""`, ""), "common の rules[0] が空"},
		{"空白だけのルール", profilesBody(`"  "`, ""), "空"},
		{"改行入りのルール", profilesBody(`"a\nb"`, ""), "1 行"},
		{"CR 入りのルール", profilesBody(`"a\rb"`, ""), "1 行"},
		{"未知のプレースホルダー", profilesBody(`"{pane} を見る"`, ""), "{pane}"},
		{"空のプレースホルダー", profilesBody(`"{}"`, ""), "{}"},
		{"二重の波括弧", profilesBody(`"{{report_path}}"`, ""), "プレースホルダー"},
		{"位置のプレースホルダー", profilesBody(`"x"`, "[profile.docs]\nrules=[\"{pos}\"]\n"), "profile.docs"},
		{"プロファイル名", profilesBody(`"x"`, "[profile.\"a b\"]\nrules=[\"x\"]\n"), "プロファイル名"},
		{"未知のキー", profilesBody(`"x"`, "[profile.docs]\nrule = [\"x\"]\n"), "未知のキー"},
		{"未知のトップキー", profilesBody(`"x"`, "[other]\na=1\n"), "未知のキー"},
		{"壊れた TOML", "[common\n", "TOML"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profiles.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			p, err := LoadProfiles(path)
			if err == nil {
				t.Fatalf("エラーのはず: %+v", p)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, %q を含むはず", err, tt.want)
			}
		})
	}
}

func TestLoadProfilesUnreadable(t *testing.T) {
	// ファイルが無いのとは違い、ディレクトリなど読めないものは組み込みに黙って落とさずエラー。
	if _, err := LoadProfiles(t.TempDir()); err == nil {
		t.Fatal("ディレクトリはエラーのはず")
	}
}
