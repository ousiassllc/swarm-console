package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/updatecheck"
)

// updateEnv は新しい版の確認と自己更新の外部との境。本番はゼロ値（実際のホーム・os.Executable・標準の通信路・実時計）。
// テストが、実際の GitHub・実際の ~/bin に触れずに serve と version --check を確かめるために差し替える。
var updateEnv app.UpdateEnv

// exitCode は理由を表示済みで、指定の終了コードで終わる印（version --check の 10 など）。
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("終了コード %d", int(c)) }

// runVersion は `version`（版を 1 行出すだけ。通信しない）と `version --check`（1 回だけ確認する。FR-UPD-6）。
// --check は config.toml を読まず、update_check が false でも動き、バイナリを置き換えない。
func runVersion(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "GitHub Releases の最新版を 1 回確認する（入れ替えはしない）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError("version に位置引数は要りません")
	}
	if _, err := fmt.Fprintln(stdout, version); err != nil {
		return err
	}
	if !*check {
		return nil
	}
	res := updatecheck.New(updatecheck.Options{Current: version, Transport: updateEnv.Transport}).Check(ctx)
	if _, err := fmt.Fprintln(stdout, updatecheck.Describe(res)); err != nil {
		return err
	}
	if code := updatecheck.ExitCode(res); code != 0 {
		return exitCode(code)
	}
	return nil
}
