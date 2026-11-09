//go:build darwin

package proxy

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// bindToInterface scopes a socket to an interface (IP_BOUND_IF /
// IPV6_BOUND_IF), overriding a VPN's default route.
func bindToInterface(network string, c syscall.RawConn, idx int) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		if network == "tcp4" || network == "udp4" {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, idx)
			return
		}
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, idx)
	})
	if err != nil {
		return err
	}
	return serr
}
