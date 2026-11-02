package setup

import (
	"net"
	"net/netip"

	"github.com/ousiassllc/swarm-console/internal/config"
)

// DefaultPort は listen_addr の候補に付けるポート。
const DefaultPort = 48120

// Candidates は addrs のうち tailnet の範囲（100.64.0.0/10・fd7a:115c:a1e0::/48）にあるものに
// ポート 48120 を付けた `IP:ポート` を、現れた順・重複なしで返す（P-33）。外部コマンドも通信もしない（NFR-OPS-9）。
func Candidates(addrs []net.Addr) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if !config.IsTailnet(addr) {
			continue
		}
		s := netip.AddrPortFrom(addr, DefaultPort).String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// HostCandidates はこのホストのネットワークインターフェースから候補を出す。
func HostCandidates() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return Candidates(addrs)
}
