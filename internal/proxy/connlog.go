package proxy

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// ConnEvent is one client connection attempt, kept in memory so the
// dashboard can show why an app (an anti-detect browser's "check proxy",
// say) cannot use a proxy: refused by sharing scope, wrong password, the
// site has no IPv6, …
type ConnEvent struct {
	At     int64  `json:"at"`
	Client string `json:"client"`           // source IP of the app
	Proto  string `json:"proto"`            // http | connect | socks5 | ?
	Target string `json:"target,omitempty"` // host:port the app asked for
	Result string `json:"result"`           // ok | scope | auth_missing | auth | conn_limit | protocol | <Classify code>
	Via    string `json:"via,omitempty"`    // ipv6 | ipv4 | nat64
	Ms     int    `json:"ms,omitempty"`     // time to connect to the target
	Detail string `json:"detail,omitempty"`
}

const connLogSize = 60

type connLog struct {
	mu   sync.Mutex
	buf  []ConnEvent
	next int
}

func (c *connLog) add(e ConnEvent) {
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buf) < connLogSize {
		c.buf = append(c.buf, e)
		return
	}
	c.buf[c.next] = e
	c.next = (c.next + 1) % connLogSize
}

// list returns the events, newest first.
func (c *connLog) list() []ConnEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ConnEvent, 0, len(c.buf))
	for i := len(c.buf) - 1; i >= 0; i-- {
		out = append(out, c.buf[(c.next+i)%len(c.buf)])
	}
	return out
}

func clientIP(a net.Addr) string {
	if ap, err := netip.ParseAddrPort(a.String()); err == nil {
		return ap.Addr().Unmap().String()
	}
	return a.String()
}

// ConnLog returns the recent connection attempts of the proxy with this ID,
// newest first (nil when it does not run here).
func (m *Manager) ConnLog(id int64) []ConnEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.listeners {
		if l.cfg.Load().ID == id {
			return l.log.list()
		}
	}
	return nil
}

// record logs the outcome of a dial for one client request.
func (l *listener) record(client net.Addr, proto, target, via string, start time.Time, err error) {
	e := ConnEvent{Client: clientIP(client), Proto: proto, Target: target, Via: via, Result: "ok", Ms: int(time.Since(start).Milliseconds())}
	if err != nil {
		e.Result, e.Detail, e.Via = Classify(err), err.Error(), ""
	}
	l.log.add(e)
}
