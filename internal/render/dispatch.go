package render

import (
	"strings"

	"github.com/ousiassllc/swarm-console/internal/apperr"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// DispatchInput は指示文の組み立ての入力（API 仕様 §5.6）。
type DispatchInput struct {
	Command    string // 1 行目のスラッシュコマンド。空なら省略
	Task       string
	Profile    string
	ExtraRules []string
	Profiles   config.Profiles
	ReportDir  string
	Pos        string // 送り先ペインの位置（{report_path} の置換に使う）
}

const reportPathVar = "{report_path}"

// Dispatch は指示文を組み立てる。見出しと profiles.toml の条件文以外の文は足さない。
// 未知の Profile は apperr.InvalidArgs（details.field = "profile"）。
// {report_path} は条件（共通・プロファイル・extra_rules）の中だけ実パスに置換する。
func Dispatch(in DispatchInput) (string, error) {
	prof, ok := in.Profiles.Profiles[in.Profile]
	if !ok {
		return "", apperr.New(apperr.InvalidArgs, "field", "profile")
	}
	var b strings.Builder
	if in.Command != "" {
		b.WriteString(in.Command + "\n\n")
	}
	b.WriteString("## やること\n" + in.Task + "\n\n## 条件\n")
	path := ReportPath(in.ReportDir, in.Pos)
	for _, group := range [][]string{in.Profiles.Common, prof.Rules, in.ExtraRules} {
		for _, r := range group {
			b.WriteString("- " + strings.ReplaceAll(r, reportPathVar, path) + "\n")
		}
	}
	return b.String(), nil
}
