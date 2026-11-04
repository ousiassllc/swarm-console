package main

import (
	"flag"
	"io"
	"strings"

	"github.com/ousiassllc/swarm-console/internal/app"
	"github.com/ousiassllc/swarm-console/internal/auth"
)

// runToken は token add / token revoke / token list。tokens.toml は --config と同じフォルダ。
func runToken(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("token add・token revoke・token list のどれかが必要です")
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
		sc := auth.Scope(*scope)
		if sc != auth.ScopeOps && sc != auth.ScopeReport {
			return usageError("--scope は ops か report にしてください")
		}
		return app.TokenAdd(*configPath, pos[0], sc, list, stdout, stderr)
	case "revoke":
		pos, err := parseFlags(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("token revoke には名前を 1 つ指定してください")
		}
		return app.TokenRevoke(*configPath, pos[0], stderr)
	case "list":
		pos, err := parseFlags(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 0 {
			return usageError("token list に位置引数はありません: %q", pos[0])
		}
		return app.TokenList(*configPath, stdout, stderr)
	}
	return usageError("token の下は add・revoke・list のどれかです: %q", args[0])
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
