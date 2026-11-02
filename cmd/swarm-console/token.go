package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

// runToken は token add / token revoke。tokens.toml は --config と同じフォルダ。
func runToken(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("token add か token revoke が必要です")
	}
	fs := flag.NewFlagSet("token "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath, err := defaultConfig(fs)
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		scope := fs.String("scope", "", "窓口の範囲（ops か report）")
		sessions := fs.String("sessions", "", "触れる tmux セッション（カンマ区切り。省略は config.toml の sessions すべて）")
		pos, err := parseFlags(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("token add には名前を 1 つ指定してください")
		}
		var list []string
		if flagSet(fs, "sessions") {
			if list, err = parseSessions(*sessions); err != nil {
				return err
			}
		}
		return tokenAdd(*configPath, pos[0], auth.Scope(*scope), list, stdout, stderr)
	case "revoke":
		pos, err := parseFlags(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("token revoke には名前を 1 つ指定してください")
		}
		return tokenRevoke(*configPath, pos[0], stderr)
	}
	return usageError("token の下は add か revoke です: %q", args[0])
}

// tokenAdd は新しいトークンを発行する。平文は標準出力へ 1 度だけ（保存しない）。案内は標準エラーへ。
// sessions があれば、そのセッションだけを許可する（config.toml の sessions に含まれるものに限る）。無ければ全セッション。
func tokenAdd(configPath, name string, scope auth.Scope, sessions []string, stdout, stderr io.Writer) error {
	if scope != auth.ScopeOps && scope != auth.ScopeReport {
		return usageError("--scope は ops か report にしてください")
	}
	path, err := app.TokensPath(configPath)
	if err != nil {
		return err
	}
	if len(sessions) > 0 {
		if err := app.CheckTokenSessions(configPath, sessions); err != nil {
			return err
		}
	}
	plain, err := auth.Add(path, name, scope, time.Now(), sessions...)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, plain); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "トークン %q（%s）を発行しました。平文は今回しか表示されません。\n", name, scope)
	return nil
}

// flagSet は fs で名前 name のフラグが明示されたか。
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// parseSessions は --sessions（カンマ区切り）を分ける。空の要素は使い方の誤り。
func parseSessions(s string) ([]string, error) {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		if parts[i] = strings.TrimSpace(p); parts[i] == "" {
			return nil, usageError("--sessions はカンマ区切りのセッション名です（例: 0,1）")
		}
	}
	return parts, nil
}

// tokenRevoke はトークンを失効させる（再起動なしで次のリクエストから効く。NFR-SEC-6）。
func tokenRevoke(configPath, name string, stderr io.Writer) error {
	path, err := app.TokensPath(configPath)
	if err != nil {
		return err
	}
	if err := auth.Revoke(path, name, time.Now()); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "トークン %q を失効しました。\n", name)
	return nil
}
