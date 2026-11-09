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
	"io"
	"log"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
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
}

// SetAllow sets the client source whitelist used by proxies without auth.
func (m *Manager) SetAllow(p []netip.Prefix) { m.allow.Store(&p) }

func (m *Manager) clientAllowed(addr net.Addr) bool {
	ap, err := netip.ParseAddrPort(addr.String())
	if err != nil {
		return false
	}
	a := ap.Addr().Unmap()
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
		if !ok || l.cfg.Load().ID != c.ID {
			l.close()
			delete(m.listeners, port)
		}
	}
	for port, c := range want {
		c := c
		if l, ok := m.listeners[port]; ok {
			old := l.cfg.Load()
			l.cfg.Store(&c)
			if old.OutIP != c.OutIP || old.User != c.User || old.Pass != c.Pass {
				l.dropConns() // force clients onto the new identity
			}
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			errs[c.ID] = err
			continue
		}
		l := &listener{m: m, ln: ln, counters: &Counters{}}
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
	if !cfg.authRequired() && !l.m.clientAllowed(c.RemoteAddr()) {
		return // silently drop: not whitelisted and no credentials configured
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

func (l *listener) dial(ctx context.Context, cfg *Config, host string, port int) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a.Unmap()}
	} else {
		ips, err := l.m.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			addrs = append(addrs, ip.Unmap())
		}
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
	for _, a := range primary {
		d := net.Dialer{LocalAddr: &net.TCPAddr{IP: cfg.OutIP.AsSlice()}}
		c, err := d.DialContext(ctx, netw, netip.AddrPortFrom(a, uint16(port)).String())
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	if cfg.IPv4Fallback && cfg.OutIP.Is6() {
		for _, a := range other {
			var d net.Dialer
			c, err := d.DialContext(ctx, "tcp4", netip.AddrPortFrom(a, uint16(port)).String())
			if err == nil {
				return c, nil
			}
			lastErr = err
		}
	}
	return nil, lastErr
}

// pipe copies in both directions and updates counters.
func (l *listener) pipe(client, target net.Conn) {
	_ = client.SetDeadline(time.Time{})
	l.conns.Store(target, struct{}{})
	defer l.conns.Delete(target)
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(target, client)
		l.counters.Up.Add(n)
		closeWrite(target)
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(client, target)
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
