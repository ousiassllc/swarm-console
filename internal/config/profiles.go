package config

import (
	_ "embed" // 組み込みの初期値（profiles.default.toml）
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
)

//go:embed profiles.default.toml
var defaultProfilesTOML []byte

var (
	placeholderRe    = regexp.MustCompile(`\{[^{}]*\}`)
	profileNameRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	requiredProfiles = []string{"sweep", "fix", "investigate"}
)

const reportPathPlaceholder = "{report_path}"

type rawRules struct {
	Rules []string `toml:"rules"`
}

type rawProfiles struct {
	Common  *rawRules           `toml:"common"`
	Profile map[string]rawRules `toml:"profile"`
}

// LoadProfiles は profiles.toml を読んで検証する。ファイルが無い（または path が空）なら組み込みの初期値を返す。
// 検証（ER §6）: common と sweep・fix・investigate が必須。各 rules は空でない 1 行。
// プレースホルダーは {report_path} だけ。他の {...} はエラー。未知のキーも拒否。
func LoadProfiles(path string) (*Profiles, error) {
	data := defaultProfilesTOML
	if path != "" {
		b, err := readLimited(path)
		switch {
		case err == nil:
			data = b
		case !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("profiles: %s を読めません: %w", path, err)
		}
	}
	var raw rawProfiles
	if err := decodeStrict(data, &raw); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	p, err := buildProfiles(&raw)
	if err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	return p, nil
}

func buildProfiles(raw *rawProfiles) (*Profiles, error) {
	if raw.Common == nil {
		return nil, errors.New("[common] がありません")
	}
	if err := checkRules("common", raw.Common.Rules); err != nil {
		return nil, err
	}
	out := &Profiles{Common: append([]string{}, raw.Common.Rules...), Profiles: make(map[string]Profile, len(raw.Profile))}
	for _, name := range requiredProfiles {
		if _, ok := raw.Profile[name]; !ok {
			return nil, fmt.Errorf("[profile.%s] がありません（sweep・fix・investigate は必須）", name)
		}
	}
	for name, r := range raw.Profile {
		if !profileNameRe.MatchString(name) {
			return nil, fmt.Errorf("プロファイル名 %q は英字で始まる英数字・_・- にしてください", name)
		}
		if err := checkRules("profile."+name, r.Rules); err != nil {
			return nil, err
		}
		out.Profiles[name] = Profile{Name: name, Rules: append([]string{}, r.Rules...)}
	}
	return out, nil
}

// checkRules は rules の各要素を検査する（空でない 1 行・プレースホルダーは {report_path} だけ）。
func checkRules(where string, rules []string) error {
	for i, r := range rules {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("%s の rules[%d] が空です", where, i)
		}
		if strings.IndexFunc(r, unicode.IsControl) >= 0 {
			return fmt.Errorf("%s の rules[%d] に改行・制御文字は使えません（1 行で書く）", where, i)
		}
		if strings.Contains(r, "{{") || strings.Contains(r, "}}") {
			return fmt.Errorf("%s の rules[%d] に {{ または }} は使えません（プレースホルダーは %s だけ）", where, i, reportPathPlaceholder)
		}
		for _, ph := range placeholderRe.FindAllString(r, -1) {
			if ph != reportPathPlaceholder {
				return fmt.Errorf("%s の rules[%d] に使えないプレースホルダー %s があります（使えるのは %s だけ）", where, i, ph, reportPathPlaceholder)
			}
		}
	}
	return nil
}
