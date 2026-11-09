//go:build !windows && !darwin && !linux

package proxy

import "syscall"

// bindToInterface is a no-op: on Linux VPN bypass uses policy routing.
func bindToInterface(network string, c syscall.RawConn, idx int) error { return nil }
