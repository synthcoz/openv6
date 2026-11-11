// Package proxy implements the per-port HTTP + SOCKS5 proxy engine. Each
// listener has its own outbound IPv6 address; the protocol is detected from the
// first byte of the client connection (0x05 = SOCKS5, otherwise HTTP).
package proxy

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/time/rate"
	"time"
)

// Config of one proxy listener. Replaced atomically on update.
type Config struct {
	ID           int64
	Port         int
	Protocol     string // both | http | socks5
	User, Pass   string
	OutIP        netip.Addr // outbound IPv6 source
	IPv4Fallback bool
	// Scope controls who may connect: "local" (this machine only, bound to
	// 127.0.0.1), "lan" (node whitelist, default private ranges) or "any"
	// (anyone with the password; without a password it falls back to lan).
	Scope string
	// Resolver used for target hostnames; nil = system resolver.
	Resolver *net.Resolver
	// MaxConns limits simultaneous client connections (0 = unlimited).
	MaxConns int
	// RateKBps limits throughput per direction in KiB/s (0 = unlimited).
	RateKBps int
	// BindIf, when > 0, pins outgoing sockets to this interface index
	// (Windows/macOS VPN bypass; ignored on Linux).
	BindIf int
	// Block lists destinations this proxy refuses (dashboard: Blocked sites).
	Block *Blocklist
	// DoH, when set, is asked for the IPv6 address of a site whose normal
	// DNS answer has none (ISP/VPN resolvers that drop AAAA records).
	DoH *DoH
	// IPv4Sites may use IPv4 even with IPv4Fallback off (e.g. the IPv4-only
	// "check proxy" endpoint of an anti-detect browser).
	IPv4Sites *Blocklist
	// NAT64, when valid (a /96), carries IPv4-only destinations over IPv6
	// through a NAT64 gateway instead of this machine's IPv4: the site sees
	// the gateway's IPv4 (e.g. your VPS), never your own.
	NAT64 netip.Prefix
}

// Dialer returns a net.Dialer for this proxy's outbound connections.
func (c *Config) Dialer(local net.Addr) *net.Dialer {
	d := &net.Dialer{LocalAddr: local}
	if c.BindIf > 0 {
		idx := c.BindIf
		d.Control = func(network, _ string, rc syscall.RawConn) error { return bindToInterface(network, rc, idx) }
	}
	return d
}

// BindDialer is Dialer for callers outside the package (health checks, DNS).
func BindDialer(local net.Addr, ifIndex int) *net.Dialer {
	c := Config{BindIf: ifIndex}
	return c.Dialer(local)
}

func (c *Config) bindAddr() string {
	if c.Scope == "local" {
		return fmt.Sprintf("127.0.0.1:%d", c.Port)
	}
	return fmt.Sprintf(":%d", c.Port)
}

func (c *Config) authRequired() bool { return c.User != "" }

func (c *Config) checkAuth(u, p string) bool {
	return subtle.ConstantTimeCompare([]byte(u), []byte(c.User)) == 1 &&
		subtle.ConstantTimeCompare([]byte(p), []byte(c.Pass)) == 1
}

// Counters for one proxy. Up = client -> target, Down = target -> client.
type Counters struct {
	Up, Down, Conns, Active atomic.Int64
}

// Manager owns all listeners on a node.
type Manager struct {
	mu        sync.Mutex
	listeners map[int]*listener // by port
	allow     atomic.Pointer[[]netip.Prefix]
	Resolver  *net.Resolver
	Logf      func(format string, args ...any)
}

func NewManager() *Manager {
	m := &Manager{listeners: map[int]*listener{}, Resolver: net.DefaultResolver, Logf: log.Printf}
	empty := []netip.Prefix{}
	m.allow.Store(&empty)
	return m
}

type listener struct {
	m        *Manager
	ln       net.Listener
	cfg      atomic.Pointer[Config]
	counters *Counters
	closed   atomic.Bool
	conns    sync.Map // net.Conn -> struct{}
	upLim    atomic.Pointer[rate.Limiter]
	downLim  atomic.Pointer[rate.Limiter]
	log      connLog
}

func (l *listener) setLimits(c *Config) {
	old := l.cfg.Load()
	if old != nil && old.RateKBps == c.RateKBps && l.upLim.Load() != nil == (c.RateKBps > 0) {
		return
	}
	l.upLim.Store(limiterFor(c.RateKBps))
	l.downLim.Store(limiterFor(c.RateKBps))
}

// SetAllow sets the client source whitelist used by proxies without auth.
func (m *Manager) SetAllow(p []netip.Prefix) { m.allow.Store(&p) }

func (m *Manager) clientAllowed(cfg *Config, addr net.Addr) bool {
	ap, err := netip.ParseAddrPort(addr.String())
	if err != nil {
		return false
	}
	a := ap.Addr().Unmap()
	if a.IsLoopback() {
		return true
	}
	switch cfg.Scope {
	case "local":
		return false
	case "any":
		if cfg.authRequired() {
			return true
		}
	}
	for _, p := range *m.allow.Load() {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Apply reconciles running listeners with the desired configs. It returns a
// per-proxy error map (listen failures).
func (m *Manager) Apply(cfgs []Config) map[int64]error {
	m.mu.Lock()
	defer m.mu.Unlock()
	errs := map[int64]error{}
	want := map[int]Config{}
	for _, c := range cfgs {
		want[c.Port] = c
	}
	for port, l := range m.listeners {
		c, ok := want[port]
		if !ok || l.cfg.Load().ID != c.ID || l.cfg.Load().bindAddr() != c.bindAddr() {
			l.close()
			delete(m.listeners, port)
		}
	}
	for port, c := range want {
		c := c
		if l, ok := m.listeners[port]; ok {
			old := l.cfg.Load()
			l.setLimits(&c)
			l.cfg.Store(&c)
			if old.OutIP != c.OutIP || old.User != c.User || old.Pass != c.Pass {
				l.dropConns() // force clients onto the new identity
			}
			continue
		}
		ln, err := net.Listen("tcp", c.bindAddr())
		if err != nil {
			errs[c.ID] = err
			continue
		}
		l := &listener{m: m, ln: ln, counters: &Counters{}}
		l.setLimits(&c)
		l.cfg.Store(&c)
		m.listeners[port] = l
		go l.serve()
	}
	return errs
}

// Counters returns the counters for every running proxy (by proxy ID).
func (m *Manager) Counters() map[int64]*Counters {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]*Counters{}
	for _, l := range m.listeners {
		out[l.cfg.Load().ID] = l.counters
	}
	return out
}

func (m *Manager) Close() {
	m.Apply(nil)
}

func (l *listener) close() {
	l.closed.Store(true)
	_ = l.ln.Close()
	l.dropConns()
}

func (l *listener) dropConns() {
	l.conns.Range(func(k, _ any) bool { _ = k.(net.Conn).Close(); return true })
}

func (l *listener) serve() {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			if l.closed.Load() {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			l.m.Logf("proxy accept: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go l.handle(c)
	}
}

func (l *listener) handle(c net.Conn) {
	l.conns.Store(c, struct{}{})
	l.counters.Conns.Add(1)
	l.counters.Active.Add(1)
	defer func() {
		l.conns.Delete(c)
		l.counters.Active.Add(-1)
		_ = c.Close()
	}()
	cfg := l.cfg.Load()
	if !l.m.clientAllowed(cfg, c.RemoteAddr()) {
		// silently drop: outside the proxy's sharing scope
		l.log.add(ConnEvent{Client: clientIP(c.RemoteAddr()), Proto: "?", Result: "scope", Detail: "scope " + cfg.Scope})
		return
	}
	if cfg.MaxConns > 0 && l.counters.Active.Load() > int64(cfg.MaxConns) {
		l.log.add(ConnEvent{Client: clientIP(c.RemoteAddr()), Proto: "?", Result: "conn_limit"})
		return // over the connection limit
	}
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	bc := &bufConn{Conn: c, r: br}
	if first[0] == 0x05 {
		if cfg.Protocol == "http" {
			return
		}
		l.serveSOCKS5(bc, cfg)
	} else {
		if cfg.Protocol == "socks5" {
			return
		}
		l.serveHTTP(bc, cfg)
	}
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// ---- outbound dialing ----

var ErrNoIPv6 = errors.New("target has no IPv6 address")

// ErrBlocked: proxies must not be a way into this machine, the LAN or a
// cloud metadata service (and no hop from one proxy into another one's
// loopback listener, which skips its password).
var ErrBlocked = errors.New("destination is a local or private address (blocked for safety)")

var blockedNets = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "64:ff9b:1::/48"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// publicAddr reports whether a destination may be dialed.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range blockedNets {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// wellKnownNAT64 is the RFC 6052 prefix many networks and DNS64 resolvers use.
var wellKnownNAT64 = netip.MustParsePrefix("64:ff9b::/96")

// embeddedV4 returns the IPv4 address carried in a NAT64 address (the
// well-known prefix or the configured one).
func (c *Config) embeddedV4(a netip.Addr) (netip.Addr, bool) {
	if !a.Is6() || !(wellKnownNAT64.Contains(a) || (c.NAT64.IsValid() && c.NAT64.Contains(a))) {
		return netip.Addr{}, false
	}
	b := a.As16()
	return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
}

// allowPrivateTargets lets tests reach loopback servers; never set in production.
var allowPrivateTargets = false

// targetAllowed reports whether the proxy may connect to a. A NAT64 address
// is judged by the IPv4 inside it: the gateway would otherwise carry clients
// to its own loopback, its LAN or a cloud metadata service. Checks are strict
// when the proxy goes out on a public address (always in production); lab
// setups with private outbound addresses are left alone.
func (c *Config) targetAllowed(a netip.Addr) bool {
	a = a.Unmap()
	if a.Zone() != "" {
		return false
	}
	if allowPrivateTargets || !publicAddr(c.OutIP) {
		return true
	}
	if v4, ok := c.embeddedV4(a); ok {
		return publicAddr(v4)
	}
	return publicAddr(a)
}

// filterTargets keeps the destinations this proxy may connect to.
func (c *Config) filterTargets(addrs []netip.Addr) ([]netip.Addr, bool) {
	var keep []netip.Addr
	for _, a := range addrs {
		if c.targetAllowed(a) {
			keep = append(keep, a)
		}
	}
	return keep, len(keep) < len(addrs)
}

// blockedAddr checks the blocked-sites list, including the IPv4 inside a
// NAT64 address.
func (c *Config) blockedAddr(a netip.Addr) bool {
	if c.Block.Addr(a) {
		return true
	}
	v4, ok := c.embeddedV4(a)
	return ok && c.Block.Addr(v4)
}

func (l *listener) dial(ctx context.Context, cfg *Config, host string, port int) (net.Conn, error) {
	c, _, err := l.dialVia(ctx, cfg, host, port)
	return c, err
}

// nat64Addr embeds an IPv4 address in a NAT64 /96 prefix (RFC 6052).
func nat64Addr(p netip.Prefix, v4 netip.Addr) netip.Addr {
	b := p.Addr().As16()
	a4 := v4.As4()
	copy(b[12:], a4[:])
	return netip.AddrFrom16(b)
}

// dialVia connects to host:port the way this proxy may and also reports how
// the connection left: "ipv6", "ipv4" (this machine's IPv4) or "nat64".
func (l *listener) dialVia(ctx context.Context, cfg *Config, host string, port int) (net.Conn, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if strings.Contains(host, "%") { // IPv6 zone: would slip past range checks
		return nil, "", ErrBlocked
	}
	if cfg.Block.Host(host) {
		return nil, "", ErrBlockedByRule
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a.Unmap()}
	} else {
		var err error
		if addrs, err = l.m.resolve(ctx, cfg, host); err != nil {
			return nil, "", err
		}
	}
	for _, a := range addrs {
		if cfg.blockedAddr(a) {
			return nil, "", ErrBlockedByRule
		}
	}
	addrs, dropped := cfg.filterTargets(addrs)
	if len(addrs) == 0 && dropped {
		return nil, "", ErrBlocked
	}
	// Primary family = family of the configured outbound IP (normally IPv6).
	// An IPv4 outbound IP is supported too (multi-IPv4 servers, tests).
	var primary, other []netip.Addr
	for _, a := range addrs {
		if a.Is4() == cfg.OutIP.Is4() {
			primary = append(primary, a)
		} else {
			other = append(other, a)
		}
	}
	netw := "tcp6"
	if cfg.OutIP.Is4() {
		netw = "tcp4"
	}
	var lastErr error = ErrNoIPv6
	via := "ipv6"
	if cfg.OutIP.Is4() {
		via = "ipv4"
	}
	for _, a := range primary {
		d := cfg.Dialer(&net.TCPAddr{IP: cfg.OutIP.AsSlice()})
		c, err := d.DialContext(ctx, netw, netip.AddrPortFrom(a, uint16(port)).String())
		if err == nil {
			return c, via, nil
		}
		lastErr = err
	}
	v4ok := cfg.IPv4Fallback || cfg.IPv4Sites.Host(host)
	if cfg.OutIP.Is6() {
		for _, a := range other {
			if !a.Is4() || !(v4ok || cfg.IPv4Sites.Addr(a)) {
				continue
			}
			// leaves from the machine's IPv4 or the NAT64 gateway: never to a
			// local destination, whatever the proxy's own address is
			if !publicAddr(a) && !allowPrivateTargets {
				lastErr = ErrBlocked
				continue
			}
			var c net.Conn
			var err error
			if cfg.NAT64.IsValid() { // IPv4 site through the NAT64 gateway, still from the proxy's IPv6
				d := cfg.Dialer(&net.TCPAddr{IP: cfg.OutIP.AsSlice()})
				c, err = d.DialContext(ctx, "tcp6", netip.AddrPortFrom(nat64Addr(cfg.NAT64, a), uint16(port)).String())
				via = "nat64"
			} else {
				var d net.Dialer
				c, err = d.DialContext(ctx, "tcp4", netip.AddrPortFrom(a, uint16(port)).String())
				via = "ipv4"
			}
			if err == nil {
				return c, via, nil
			}
			lastErr = err
		}
	}
	return nil, "", lastErr
}

// resolve looks a host up with the proxy's resolver. When the answer has no
// IPv6 address (or the lookup fails) and encrypted DNS is enabled, it asks
// DNS-over-HTTPS for the AAAA record too: many ISP routers and VPN resolvers
// drop AAAA answers for sites that do have IPv6.
func (m *Manager) resolve(ctx context.Context, cfg *Config, host string) ([]netip.Addr, error) {
	res := m.Resolver
	if cfg.Resolver != nil {
		res = cfg.Resolver
	}
	ips, err := res.LookupNetIP(ctx, "ip", host)
	var addrs []netip.Addr
	has6 := false
	for _, ip := range ips {
		ip = ip.Unmap()
		addrs = append(addrs, ip)
		has6 = has6 || ip.Is6()
	}
	if cfg.DoH != nil && cfg.OutIP.Is6() && !has6 {
		if v6, derr := cfg.DoH.Lookup(ctx, host, TypeAAAA, cfg.OutIP, cfg.BindIf); derr == nil && len(v6) > 0 {
			return append(v6, addrs...), nil
		}
	}
	if err != nil && len(addrs) == 0 {
		return nil, err
	}
	return addrs, nil
}

// pipe copies in both directions and updates counters.
func (l *listener) pipe(client, target net.Conn) {
	_ = client.SetDeadline(time.Time{})
	l.conns.Store(target, struct{}{})
	defer l.conns.Delete(target)
	done := make(chan struct{}, 2)
	go func() {
		n, _ := copyLimited(target, client, l.upLim.Load())
		l.counters.Up.Add(n)
		closeWrite(target)
		done <- struct{}{}
	}()
	go func() {
		n, _ := copyLimited(client, target, l.downLim.Load())
		l.counters.Down.Add(n)
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
	// give the other direction a moment to flush, then tear down
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
	_ = target.Close()
	_ = client.Close()
}

func closeWrite(c net.Conn) {
	if bc, ok := c.(*bufConn); ok {
		c = bc.Conn
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	} else {
		_ = c.Close()
	}
}
