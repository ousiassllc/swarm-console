package listentests

import (
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/setup"
)

func TestCheckListenAddr(t *testing.T) {
	ok := []struct{ in, want string }{
		{"100.64.0.1:48120", "100.64.0.1:48120"},
		{"100.127.255.254:48120", "100.127.255.254:48120"},
		{"127.0.0.1:48120", "127.0.0.1:48120"},
		{"127.0.0.1:0", "127.0.0.1:0"}, // テスト用の空きポート
		{"[::1]:48120", "[::1]:48120"},
		{"[fd7a:115c:a1e0::1]:48120", "[fd7a:115c:a1e0::1]:48120"},
		{"[fd7a:115c:a1e0:ab12:4843:cd96:625d:1234]:48120", "[fd7a:115c:a1e0:ab12:4843:cd96:625d:1234]:48120"},
		{"127.0.0.1:8080", "127.0.0.1:8080"}, // ループバックはどのポートでもよい
		{"[::1]:1", "[::1]:1"},
		{"[::ffff:100.64.0.1]:48120", "100.64.0.1:48120"},
	}
	for _, tt := range ok {
		got, err := config.CheckListenAddr(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("%q → %q, %v（%q のはず）", tt.in, got, err, tt.want)
		}
	}
	bad := []struct{ in, why string }{
		{"", "必須"},
		{"0.0.0.0:48120", "0.0.0.0"},
		{"[::]:48120", "0.0.0.0"},
		{"[::ffff:0.0.0.0]:48120", "0.0.0.0"},
		{":48120", "ホスト名"},
		{"*:48120", "ホスト名"},
		{"localhost:48120", "ホスト名"},
		{"example.com:48120", "ホスト名"},
		{"100.64.0.1", "ポート無し"},
		{"[fd7a:115c:a1e0::1]", "ポート無し"},
		{"::", "ポート無し"},
		{"100.64.0.1:", "ポート無し"},
		{"100.64.0.1:abc", "ポート"},
		{"100.64.0.1:65536", "ポート"},
		{"100.64.0.1:0", "ポート"},
		{"100.64.0.1:8080", "ポート"}, // tailnet の IP は 48120 だけ
		{"100.64.0.1:1", "ポート"},
		{"[fd7a:115c:a1e0::1]:1", "ポート"},
		{"[::ffff:100.64.0.1]:8080", "ポート"},
		{"100.63.255.255:48120", "tailnet"},
		{"100.128.0.0:48120", "tailnet"},
		{"192.168.1.1:48120", "tailnet"},
		{"10.0.0.1:48120", "tailnet"},
		{"8.8.8.8:48120", "tailnet"},
		{"[fd7a:115c:a1e1::1]:48120", "tailnet"},
		{"[fe80::1]:48120", "tailnet"},
		{"[fe80::1%eth0]:48120", "ゾーン"},
		{" 100.64.0.1:48120", "ホスト名"},
		{"http://100.64.0.1:48120", "ホスト名"},
	}
	for _, tt := range bad {
		if got, err := config.CheckListenAddr(tt.in); err == nil {
			t.Errorf("%q は拒否のはずが %q", tt.in, got)
		}
	}
	// 理由の代表（メッセージが違う拒否理由を指していること）。
	if _, err := config.CheckListenAddr("0.0.0.0:1"); err == nil || !strings.Contains(err.Error(), "0.0.0.0") {
		t.Errorf("0.0.0.0 の理由: %v", err)
	}
	if _, err := config.CheckListenAddr("100.64.0.1:8080"); err == nil || !strings.Contains(err.Error(), "48120") {
		t.Errorf("ポートの理由: %v", err)
	}
	if _, err := config.CheckListenAddr("192.168.1.1:1"); err == nil || !strings.Contains(err.Error(), "tailnet") {
		t.Errorf("範囲外の理由: %v", err)
	}
}

// setup が候補に出す IP はすべて config.CheckListenAddr を通る（setup が出した値を serve が拒否しない）。
// 範囲の端と外側を含め、候補に出るかどうかが config.IsTailnet と一致することも確かめる。
func TestSetupCandidatesPassCheckListenAddr(t *testing.T) {
	ips := []string{
		"100.63.255.255", "100.64.0.0", "100.64.0.1", "100.127.255.255", "100.128.0.0",
		"fd7a:115c:a1e0::", "fd7a:115c:a1e0:ffff:ffff:ffff:ffff:ffff", "fd7a:115c:a1e1::", "fd7a:115c:a1df:ffff::",
		"127.0.0.1", "192.168.1.5", "fe80::1", "::ffff:100.64.0.1",
	}
	for _, s := range ips {
		got := setup.Candidates([]net.Addr{&net.IPAddr{IP: net.ParseIP(s)}})
		want := config.IsTailnet(netip.MustParseAddr(s).Unmap())
		if (len(got) == 1) != want {
			t.Fatalf("%s: 候補 %v, IsTailnet %v", s, got, want)
		}
		for _, c := range got {
			if _, err := config.CheckListenAddr(c); err != nil {
				t.Fatalf("%s: 候補 %q が CheckListenAddr を通らない: %v", s, c, err)
			}
		}
	}
}
