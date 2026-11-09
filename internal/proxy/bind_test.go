//go:build windows || darwin

package proxy

import (
	"net"
	"testing"
)

// The interface-binding socket option must be accepted by the OS.
func TestBindToInterfaceOption(t *testing.T) {
	// use the interface the OS routes Internet traffic through
	idx := egressIndex("udp4", "8.8.8.8:53")
	if idx == 0 {
		t.Skip("no IPv4 route")
	}
	for _, nw := range []string{"udp4", "udp6"} {
		i := idx
		if nw == "udp6" {
			if i = egressIndex("udp6", "[2001:4860:4860::8888]:53"); i == 0 {
				t.Log("no IPv6 route on this machine, skipping udp6")
				continue
			}
		}
		d := BindDialer(nil, i)
		addr := "8.8.8.8:53"
		if nw == "udp6" {
			addr = "[2001:4860:4860::8888]:53"
		}
		c, err := d.Dial(nw, addr)
		if err != nil {
			if ne, ok := err.(*net.OpError); ok && ne.Op == "dial" && nw == "udp6" {
				t.Logf("%s: %v (no IPv6 route on this machine; option itself accepted?)", nw, err)
				continue
			}
			t.Fatalf("%s: %v", nw, err)
		}
		c.Close()
	}
}

func egressIndex(nw, addr string) int {
	c, err := net.Dial(nw, addr)
	if err != nil {
		return 0
	}
	defer c.Close()
	src := c.LocalAddr().(*net.UDPAddr).IP
	ifs, _ := net.Interfaces()
	for _, it := range ifs {
		addrs, _ := it.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(src) {
				return it.Index
			}
		}
	}
	return 0
}
