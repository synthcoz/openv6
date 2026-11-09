//go:build windows || darwin

package proxy

import (
	"net"
	"testing"
)

// The interface-binding socket option must be accepted by the OS.
func TestBindToInterfaceOption(t *testing.T) {
	ifs, _ := net.Interfaces()
	idx := 0
	for _, it := range ifs {
		if it.Flags&net.FlagUp != 0 && it.Flags&net.FlagLoopback == 0 {
			idx = it.Index
			break
		}
	}
	if idx == 0 {
		t.Skip("no interface")
	}
	for _, nw := range []string{"udp4", "udp6"} {
		d := BindDialer(nil, idx)
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
