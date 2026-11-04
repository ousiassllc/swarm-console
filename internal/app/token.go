package app

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ousiassllc/swarm-console/internal/auth"
)

// TokenAdd は新しいトークンを発行する（`token add`）。平文は stdout へ 1 度だけ（保存しない）。案内は stderr へ。
// sessions があれば、そのセッションだけを許可する（config.toml の sessions に含まれるものに限る）。無ければ全セッション。
// scope の検査は呼び出し側（使い方の誤りとして扱うため）。
func TokenAdd(configPath, name string, scope auth.Scope, sessions []string, stdout, stderr io.Writer) error {
	path, err := TokensPath(configPath)
	if err != nil {
		return err
	}
	if len(sessions) > 0 {
		if err := CheckTokenSessions(configPath, sessions); err != nil {
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

// TokenRevoke はトークンを失効させる（`token revoke`。再起動なしで次のリクエストから効く。NFR-SEC-6）。
func TokenRevoke(configPath, name string, stderr io.Writer) error {
	path, err := TokensPath(configPath)
	if err != nil {
		return err
	}
	if err := auth.Revoke(path, name, time.Now()); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "トークン %q を失効しました。\n", name)
	return nil
}

// TokenList はトークンを 1 件 1 行で stdout に出す（`token list`。名前・scope・許可セッション・発行日時・状態）。
// ハッシュ・平文は出さない。許可セッションが無いもの（config.toml の sessions すべて）は all。無ければ stderr に知らせる。
func TokenList(configPath string, stdout, stderr io.Writer) error {
	path, err := TokensPath(configPath)
	if err != nil {
		return err
	}
	infos, err := auth.List(path)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		_, _ = fmt.Fprintln(stderr, "トークンはありません。")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, in := range infos {
		sessions, status := "all", "active"
		if in.Sessions != nil {
			sessions = strings.Join(in.Sessions, ",")
		}
		if in.RevokedAt != "" {
			status = "revoked " + in.RevokedAt
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", in.Name, in.Scope, sessions, in.CreatedAt, status)
	}
	return tw.Flush()
}
