//go:build linux

package proxy

import (
	"net"
	"syscall"
)

// bindToInterface pins a socket to a network card (SO_BINDTODEVICE), used
// when a proxy is set to leave through another card than the node default
// (e.g. Wi-Fi). VPN bypass on Linux uses policy routing instead.
func bindToInterface(network string, c syscall.RawConn, idx int) error {
	it, err := net.InterfaceByIndex(idx)
	if err != nil {
		return err
	}
	var serr error
	if err := c.Control(func(fd uintptr) { serr = syscall.BindToDevice(int(fd), it.Name) }); err != nil {
		return err
	}
	return serr
}
