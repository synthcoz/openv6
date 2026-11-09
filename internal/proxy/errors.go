package proxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// Classify turns a network error into a short code the UI translates into an
// explanation and a fix (err_<code> / errfix_<code>).
func Classify(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, ErrNoIPv6):
		return "no_ipv6_target"
	case errors.Is(err, syscall.EADDRINUSE):
		return "port_in_use"
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		return "addr_not_available"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "permission"
	case errors.Is(err, syscall.ENETUNREACH):
		return "net_unreachable"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "host_unreachable"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "reset"
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, syscall.ETIMEDOUT):
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "tls"), strings.Contains(msg, "certificate"):
		return "tls"
	case strings.Contains(msg, "timeout"):
		return "timeout"
	}
	return "other"
}

// NewResolver returns a resolver that queries the given DNS servers directly.
// IPv6 servers are queried from src (the proxy's own address), so with VPN
// bypass routing the lookup leaves through the same NIC as the proxy and is
// not filtered by a VPN's DNS (some VPNs drop AAAA answers).
func NewResolver(servers []string, src netip.Addr) *net.Resolver {
	var v6, v4 []string
	for _, s := range servers {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		hp := netip.AddrPortFrom(a, 53).String()
		if a.Is4() {
			v4 = append(v4, hp)
		} else {
			v6 = append(v6, hp)
		}
	}
	if len(v6)+len(v4) == 0 {
		return nil
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var lastErr error
			for _, srv := range v6 {
				d := net.Dialer{Timeout: 4 * time.Second}
				if src.Is6() {
					if strings.HasPrefix(network, "udp") {
						d.LocalAddr = &net.UDPAddr{IP: src.AsSlice()}
					} else {
						d.LocalAddr = &net.TCPAddr{IP: src.AsSlice()}
					}
				}
				c, err := d.DialContext(ctx, network, srv)
				if err == nil {
					return c, nil
				}
				lastErr = err
			}
			for _, srv := range v4 {
				d := net.Dialer{Timeout: 4 * time.Second}
				c, err := d.DialContext(ctx, network, srv)
				if err == nil {
					return c, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}
