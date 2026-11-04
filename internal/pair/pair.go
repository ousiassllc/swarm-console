// Package pair は `swarm-console pair`（swarm-viewer の接続文字列を 1 行出す。Issue #83）を持つ。
// 接続文字列を作れることを先にすべて確かめてから、report スコープの token を 1 つ発行する。
package pair

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// ErrUsage は引数の誤り（呼び出し側が使い方を添えて終了コード 2 にする）。
var ErrUsage = errors.New("引数が正しくありません")

// Help は `pair --help` の本文。
const Help = `使い方:
  swarm-console pair [--sessions 0,1] [--name <表示名>] [--config <path>]

swarm-viewer の接続文字列を 1 行だけ標準出力に出します（report スコープの token を 1 つ発行します）。

  swarm-viewer://pair?v=1&url=<URLエンコード>&token=<TOKEN>[&name=<表示名>][&sessions=0%2C1]

注意:
  - token（平文）が標準出力に入ります。コマンド履歴・端末ログ・チャットに貼らないでください。
  - token は今回しか表示されません。使い回さないでください。
  - なくしたときは "swarm-console token revoke <token 名>" で失効させ、pair をやり直してください。
  - pair が発行するのは report スコープの token だけです（ops は発行しません）。
  - url は listen_addr の tailnet の IP（ポート 48120）から作ります。決められないときは token を発行せずに終わります。

`

// Deps は token 名の自動決定に使う外部の値。テストで差し替える。
type Deps struct {
	Hostname func() (string, error)
	Now      func() time.Time
}

// Options は pair の入力。
type Options struct {
	ConfigPath string
	Sessions   []string // --sessions。nil なら config.toml の sessions すべて
	Name       string   // --name。空なら name パラメータを付けない
}

// Run は pair の引数（サブコマンド名を除く）を読んで実行する。
func Run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = io.WriteString(stderr, Help)
		fs.PrintDefaults()
	}
	def, err := app.DefaultConfigPath()
	if err != nil {
		return err
	}
	opts := Options{}
	fs.StringVar(&opts.ConfigPath, "config", def, "config.toml のパス")
	// Func は明示されたときだけ呼ばれる（`--sessions ""` や `--name ""` も検査に掛かる）。
	fs.Func("sessions", "許可セッション（カンマ区切り。省略は config.toml の sessions すべて）", func(s string) (err error) {
		opts.Sessions, err = parseSessions(s)
		return err
	})
	fs.Func("name", "接続文字列の name に入れる表示名（1〜100 文字。省略時は付けない）", func(s string) error {
		opts.Name = s
		return checkName(s)
	})
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%w: pair に位置引数はありません: %q", ErrUsage, fs.Arg(0))
	}
	return issue(opts, Deps{Hostname: os.Hostname, Now: time.Now}, stdout, stderr)
}

// parseSessions は --sessions（カンマ区切り）を分ける。空の要素は使い方の誤り。
func parseSessions(s string) ([]string, error) {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		if parts[i] = strings.TrimSpace(p); parts[i] == "" {
			return nil, errors.New("カンマ区切りのセッション名にしてください（例: 0,1）")
		}
	}
	return parts, nil
}

// issue は接続文字列を作れることを先に確かめてから token を発行し、接続文字列を stdout へ 1 行だけ出す。
// 案内は stderr へ（token は出さない）。
func issue(opts Options, deps Deps, stdout, stderr io.Writer) error {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("config.toml を読めません: %w", err)
	}
	viewerURL, err := urlFromListenAddr(cfg.ListenAddr)
	if err != nil {
		return err
	}
	hint := cfg.Sessions
	if opts.Sessions != nil {
		if err := app.CheckTokenSessions(opts.ConfigPath, opts.Sessions); err != nil {
			return err
		}
		hint = opts.Sessions
	}
	if n := len(connectionString(viewerURL, placeholderToken, opts.Name, hint)); n > maxBytes {
		return fmt.Errorf("接続文字列が %d バイトになり、上限 %d バイトを超えます（--name・--sessions を短くしてください）", n, maxBytes)
	}
	path, err := app.TokensPath(opts.ConfigPath)
	if err != nil {
		return err
	}
	tokenName, plain, err := addToken(path, tokenBase(deps), opts.Sessions, deps.Now())
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, connectionString(viewerURL, plain, opts.Name, hint)); err != nil {
		return fmt.Errorf("接続文字列を出力できませんでした。token %q は使えないので `swarm-console token revoke %s` で失効してください: %w", tokenName, tokenName, err)
	}
	_, _ = fmt.Fprintf(stderr, `report スコープの token %q を発行しました。
  - 標準出力の接続文字列に token の平文が入っています。今回しか表示されません。
  - コマンド履歴・端末ログ・チャットに貼らないでください。使い回さないでください。
  - なくしたときは "swarm-console token revoke %s" で失効させ、pair をやり直してください。
`, tokenName, tokenName)
	return nil
}
