package config_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/ousiassllc/swarm-console/internal/config"
	"github.com/ousiassllc/swarm-console/internal/setup"
)

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
