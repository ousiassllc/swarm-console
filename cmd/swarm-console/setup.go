package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/setup"
)

// runSetup は `setup [--config <path>]`。ここでは引数と TTY 判定だけを行い、画面と保存は internal/setup が行う。
// 端末でないとき（標準入力か標準出力が端末でない・TERM=dumb）は、何も読まず何も書かず、理由と手書きの案内を標準エラーへ出して終了コード 1（FR-SET-6）。
func runSetup(ctx context.Context, args []string, stdout, stderr io.Writer, in, out *os.File, termEnv string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath, err := defaultConfig(fs)
	if err != nil {
		return err
	}
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageError("setup に位置引数は要りません")
	}
	if reason := notTerminal(in, out, termEnv); reason != "" {
		_, _ = fmt.Fprintf(stderr, "setup は対話型の画面です。端末で実行してください（%s）。\n%s", reason, manualGuide(*configPath))
		return errSilent
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("ホームフォルダを決められません: %w", err)
	}
	err = setup.Run(ctx, setup.Options{
		Path: *configPath,
		Out:  stdout,
		Home: home,
		Prompter: &setup.HuhPrompter{
			In: in, Out: out, Home: home, Candidates: setup.HostCandidates(),
		},
		// token add と同じ処理（平文は標準出力へ 1 度だけ）
		IssueToken: func(name string, scope auth.Scope) error {
			return tokenAdd(*configPath, name, scope, nil, stdout, stderr)
		},
	})
	switch {
	case err == nil:
		return nil
	case setup.IsAbort(err):
		_, _ = fmt.Fprintln(stderr, "中止しました。config.toml は書き換えていません。")
		return errSilent
	}
	var fe *config.FormatError
	if errors.As(err, &fe) {
		return fmt.Errorf("%w\nconfig.toml は書き換えていません。手で直してから、もう一度 setup を実行してください", err)
	}
	return err
}

// notTerminal は端末として使えない理由を返す（使えるなら空）。
func notTerminal(in, out *os.File, termEnv string) string {
	switch {
	case !term.IsTerminal(int(in.Fd())):
		return "標準入力が端末ではありません"
	case !term.IsTerminal(int(out.Fd())):
		return "標準出力が端末ではありません"
	case termEnv == "dumb":
		return "TERM=dumb です"
	}
	return ""
}

// manualGuide は端末が無いときの手書きの案内。
func manualGuide(path string) string {
	return fmt.Sprintf(`手で書く場合は %s を作り、listen_addr（tailnet の IP:48120。テスト用はループバック）を書いてください。例:
  mkdir -p "$(dirname %s)"
  printf 'listen_addr = "100.x.y.z:48120"\n' > %s
  swarm-console token add <名前> --scope ops
詳しくは docs/environment/setup.md のローカルで動かす節を見てください。
`, path, path, path)
}
