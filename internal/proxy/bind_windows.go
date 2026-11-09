//go:build windows

package proxy

import (
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	ipUnicastIf   = 31 // IP_UNICAST_IF (IPPROTO_IP), index in network byte order
	ipv6UnicastIf = 31 // IPV6_UNICAST_IF (IPPROTO_IPV6), index in host byte order
)

// bindToInterface forces a socket out of the given interface, overriding a
// VPN's default route (Windows "unicast interface" option).
func bindToInterface(network string, c syscall.RawConn, idx int) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		h := windows.Handle(fd)
		if network == "tcp4" || network == "udp4" {
			v := int(uint32(idx)<<24 | uint32(idx)<<8&0xff0000 | uint32(idx)>>8&0xff00 | uint32(idx)>>24)
			serr = windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf, v)
			return
		}
		serr = windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIf, idx)
	})
	if err != nil {
		return err
	}
	return serr
}
