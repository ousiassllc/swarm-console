package config

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"unicode"
)

var (
	tailnetV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

	socketRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// IsTailnet は ip が tailnet の範囲（100.64.0.0/10・fd7a:115c:a1e0::/48）にあるかを返す。
// listen_addr の検証と setup の候補が同じ判定を使う。IPv4 射影アドレスは呼び出し側で Unmap しておく。
func IsTailnet(ip netip.Addr) bool {
	return tailnetV4.Contains(ip) || tailnetV6.Contains(ip)
}

// CheckListenAddr は P-01 の検証。`IP:ポート` の IP リテラルだけを受け、
// 0.0.0.0・::・ホスト名・ポート無しは拒否する。許可は tailnet の範囲かループバック。
// 返す文字列は netip で正規化したもの。
func CheckListenAddr(s string) (string, error) {
	if s == "" {
		return "", errors.New("listen_addr は必須です（tailnet の IP:ポート）")
	}
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return "", errors.New("listen_addr は IP アドレスとポートで書いてください（例 100.x.y.z:48120）。ホスト名・ポート無しは使えません")
	}
	ip := ap.Addr().Unmap()
	switch {
	case ip.Zone() != "":
		return "", errors.New("listen_addr にゾーンは付けられません")
	case ip.IsUnspecified():
		return "", errors.New("listen_addr に 0.0.0.0・:: は使えません。tailnet の IP を書いてください")
	case !ip.IsLoopback() && !IsTailnet(ip):
		return "", errors.New("listen_addr は tailnet の範囲（100.64.0.0/10・fd7a:115c:a1e0::/48）かループバックだけ許可します")
	case ap.Port() == 0 && !ip.IsLoopback():
		return "", errors.New("listen_addr のポートは 1〜65535 で書いてください")
	}
	return netip.AddrPortFrom(ip, ap.Port()).String(), nil
}

func checkSocket(s string) error {
	if s != "" && !socketRe.MatchString(s) {
		return errors.New("tmux_socket は英数字・_・-・. だけのソケット名です")
	}
	return nil
}

// CheckSessions は sessions の既定（["0"]）・重複・default_session を検査する。
// tmux はセッション名の "." と ":" を "_" に置き換えるため、それを含む名前は一致しないので拒否する。
func CheckSessions(sessions []string, def string) ([]string, string, error) {
	if sessions == nil {
		sessions = DefaultSessions()
	}
	if len(sessions) == 0 {
		return nil, "", errors.New("sessions は 1 つ以上必要です")
	}
	seen := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if strings.TrimSpace(s) == "" || strings.ContainsAny(s, ".:") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return nil, "", errors.New("sessions に使えない名前があります（空・\".\"・\":\"・制御文字は不可）")
		}
		if seen[s] {
			return nil, "", errors.New("sessions に重複があります")
		}
		seen[s] = true
	}
	switch {
	case def != "":
		if !seen[def] {
			return nil, "", errors.New("default_session は sessions のどれかにしてください")
		}
	case len(sessions) == 1:
		def = sessions[0]
	}
	return append([]string(nil), sessions...), def, nil
}

// CheckPlainLine は 1 行・制御文字なしの文字列か検査する（タイトル・コマンドなど端末へ流すもの）。
func CheckPlainLine(key, s string) error {
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return errors.New(key + " に改行・制御文字は使えません")
	}
	return nil
}
