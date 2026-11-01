// swarm-console は tmux 上のエージェントを MCP（Streamable HTTP）で見て操作するサーバー。
//
//	swarm-console serve [--config <path>]
//	swarm-console setup [--config <path>]
//	swarm-console token add <名前> --scope ops|report [--config <path>]
//	swarm-console token revoke <名前> [--config <path>]
//	swarm-console version [--check]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// version はリリースビルドで -ldflags "-X main.version=v1.0.0" により上書きする。
var version = "dev"

const usage = `使い方:
  swarm-console serve [--config <path>]
  swarm-console setup [--config <path>]
  swarm-console token add <名前> --scope ops|report [--config <path>]
  swarm-console token revoke <名前> [--config <path>]
  swarm-console version [--check]
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run はサブコマンドを実行して終了コードを返す（テストから呼べるように main から分けてある）。
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "serve":
		err = runServe(ctx, args[1:], stderr)
	case "setup":
		err = runSetup(ctx, args[1:], stdout, stderr, os.Stdin, os.Stdout, os.Getenv("TERM"))
	case "token":
		err = runToken(args[1:], stdout, stderr)
	case "version":
		err = runVersion(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, usage)
	default:
		_, _ = fmt.Fprintf(stderr, "未知のコマンド: %q\n%s", args[0], usage)
		return 2
	}
	var code exitCode
	switch {
	case err == nil:
		return 0
	case errors.As(err, &code):
		return int(code)
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errSilent):
		return 1
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "%v\n%s", err, usage)
		return 2
	}
	_, _ = fmt.Fprintf(stderr, "エラー: %v\n", err)
	return 1
}

// errSilent は理由を表示済みで、終了コード 1 で終わる印。
var errSilent = errors.New("終了")

// errUsage は引数の誤り（使い方を添えて終了コード 2）。
var errUsage = errors.New("引数が正しくありません")

func usageError(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, a...))
}

// parseFlags は fs の flag と位置引数を、順序によらず読む（`token add NAME --scope ops` も `--scope ops NAME` も可）。
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
