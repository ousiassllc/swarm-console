package pair

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/swarm-console/internal/auth"
	"github.com/ousiassllc/swarm-console/internal/config"
)

// swarm-viewer が受け付ける接続文字列の条件（Issue #83）。
const (
	scheme   = "swarm-viewer://pair"
	version  = "1"
	maxBytes = 4096 // 接続文字列全体の上限（バイト）
	maxName  = 100  // --name の上限（文字数）
	// placeholderToken は大きさの事前確認用。実際の token（sc_ ＋ base64url 43 文字）と同じ長さ。
	placeholderToken = "sc_" + "0123456789012345678901234567890123456789012"
)

// urlFromListenAddr は listen_addr（config.Load が正規化済み）から viewer が使う url を作る。
// tailnet の IP リテラルとポート 48120 だけを受け、ループバックなど決められないものはエラー（token の発行前に呼ぶ）。
// path は scope の窓口（report なら /report、ops なら /ops。viewer は url の path で種類を判断する）。
func urlFromListenAddr(listenAddr string, scope auth.Scope) (string, error) {
	ap, err := netip.ParseAddrPort(listenAddr)
	if err != nil || !config.IsTailnet(ap.Addr()) || ap.Port() != config.ListenPort {
		return "", fmt.Errorf("listen_addr %q から tailnet の IP を決められません（pair は tailnet の IP:%d だけ使えます。config.toml の listen_addr を確かめてください）", listenAddr, config.ListenPort)
	}
	return "http://" + ap.String() + "/" + string(scope), nil
}

// checkName は --name（表示名）が 1〜100 文字で、不正な UTF-8・制御文字を含まないか確かめる。
func checkName(name string) error {
	n := utf8.RuneCountInString(name)
	switch {
	case !utf8.ValidString(name):
		return errors.New("UTF-8 の文字列にしてください")
	case n < 1 || n > maxName:
		return fmt.Errorf("1〜%d 文字にしてください（%d 文字）", maxName, n)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return errors.New("改行・制御文字は使えません")
	case strings.TrimSpace(name) == "":
		return errors.New("空白だけの名前は使えません")
	}
	return nil
}

// connectionString は接続文字列を v・url・token・name・sessions の順に組み立てる（各パラメータは 1 回だけ）。
// name が空・sessions が空のときはそのパラメータを付けない。
func connectionString(viewerURL, token, name string, sessions []string) string {
	var b strings.Builder
	b.WriteString(scheme + "?v=" + version)
	b.WriteString("&url=" + queryEscape(viewerURL))
	b.WriteString("&token=" + queryEscape(token))
	if name != "" {
		b.WriteString("&name=" + queryEscape(name))
	}
	if len(sessions) > 0 {
		b.WriteString("&sessions=" + queryEscape(strings.Join(sessions, ",")))
	}
	return b.String()
}

// queryEscape は空白を + ではなく %20 にする（decodeURIComponent など + を空白と読まない実装でも同じ値になる）。
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
