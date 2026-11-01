package setup

import (
	"net"
	"reflect"
	"testing"
)

func cidr(t *testing.T, s string) net.Addr {
	t.Helper()
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip
	return n
}

func TestCandidates(t *testing.T) {
	addrs := []net.Addr{
		cidr(t, "127.0.0.1/8"),
		cidr(t, "192.168.1.5/24"),
		cidr(t, "100.64.0.1/32"),
		cidr(t, "100.127.255.254/10"),
		cidr(t, "100.128.0.1/10"), // 範囲の外
		cidr(t, "100.63.255.255/10"),
		cidr(t, "fd7a:115c:a1e0::1/128"),
		cidr(t, "fd7a:115c:a1e1::1/64"), // 範囲の外
		cidr(t, "fe80::1/64"),
		cidr(t, "100.64.0.1/32"), // 重複
		&net.IPAddr{IP: net.ParseIP("100.100.100.100")},
		&net.UnixAddr{Name: "x", Net: "unix"},
	}
	want := []string{"100.64.0.1:48120", "100.127.255.254:48120", "[fd7a:115c:a1e0::1]:48120", "100.100.100.100:48120"}
	if got := Candidates(addrs); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := Candidates(nil); len(got) != 0 {
		t.Fatalf("候補なし: %v", got)
	}
}
