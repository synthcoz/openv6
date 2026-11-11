package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// DNS record types used here.
const (
	TypeA    uint16 = 1
	TypeAAAA uint16 = 28
)

// DoHServer is one DNS-over-HTTPS endpoint, reached by IP so no other DNS is
// needed to find it; Name is checked against its TLS certificate.
type DoHServer struct {
	Addr netip.Addr
	Name string
	Path string
}

// DefaultDoH are reached over IPv6 only: a lookup never leaves from the
// machine's IPv4 address.
var DefaultDoH = []DoHServer{
	{netip.MustParseAddr("2606:4700:4700::1111"), "cloudflare-dns.com", "/dns-query"},
	{netip.MustParseAddr("2001:4860:4860::8888"), "dns.google", "/dns-query"},
	{netip.MustParseAddr("2606:4700:4700::1001"), "cloudflare-dns.com", "/dns-query"},
	{netip.MustParseAddr("2001:4860:4860::8844"), "dns.google", "/dns-query"},
}

// DoH is an encrypted DNS resolver (RFC 8484). The proxies use it as a second
// opinion when the normal resolver returns no IPv6 address: ISP routers,
// some VPNs and filtering DNS often drop AAAA answers for sites that do have
// IPv6. Queries go out over HTTPS from the proxy's own IPv6, so the ISP's DNS
// does not see them and the machine's IPv4 is never used.
type DoH struct {
	Servers []DoHServer
	Timeout time.Duration

	clients sync.Map // "src|if" -> *http.Client
	cache   sync.Map // "type|host" -> dohEntry
}

type dohEntry struct {
	addrs []netip.Addr
	exp   time.Time
}

func NewDoH() *DoH { return &DoH{Servers: DefaultDoH, Timeout: 5 * time.Second} }

func (d *DoH) client(s DoHServer, src netip.Addr, ifIndex int) *http.Client {
	key := fmt.Sprintf("%s|%d|%s", src, ifIndex, s.Name)
	if c, ok := d.clients.Load(key); ok {
		return c.(*http.Client)
	}
	var local net.Addr
	if src.IsValid() && src.Is6() {
		local = &net.TCPAddr{IP: src.AsSlice()}
	}
	dialer := BindDialer(local, ifIndex)
	dialer.Timeout = d.Timeout
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp6", addr)
		},
		TLSClientConfig:     &tls.Config{ServerName: s.Name, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: d.Timeout,
		Proxy:               nil,
	}
	c := &http.Client{Transport: tr, Timeout: d.Timeout + 2*time.Second}
	actual, _ := d.clients.LoadOrStore(key, c)
	return actual.(*http.Client)
}

// Lookup returns the addresses of one record type (TypeA or TypeAAAA). src is
// the IPv6 the query leaves from (zero = the system's choice of IPv6).
func (d *DoH) Lookup(ctx context.Context, host string, qtype uint16, src netip.Addr, ifIndex int) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	key := fmt.Sprintf("%d|%s", qtype, host)
	if e, ok := d.cache.Load(key); ok && time.Now().Before(e.(dohEntry).exp) {
		return e.(dohEntry).addrs, nil
	}
	q, err := dnsQuery(host, qtype)
	if err != nil {
		return nil, err
	}
	var lastErr error = errors.New("no DNS-over-HTTPS server answered")
	for _, s := range d.Servers {
		addrs, ttl, err := d.ask(ctx, s, q, qtype, src, ifIndex)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		ttl = min(max(ttl, 30), 600)
		d.cache.Store(key, dohEntry{addrs: addrs, exp: time.Now().Add(time.Duration(ttl) * time.Second)})
		return addrs, nil
	}
	return nil, lastErr
}

func (d *DoH) ask(ctx context.Context, s DoHServer, q []byte, qtype uint16, src netip.Addr, ifIndex int) ([]netip.Addr, uint32, error) {
	c := d.client(s, src, ifIndex)
	url := "https://" + netip.AddrPortFrom(s.Addr, 443).String() + s.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(q))
	if err != nil {
		return nil, 0, err
	}
	req.Host = s.Name
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	res, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("DNS-over-HTTPS %s: HTTP %d", s.Name, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return nil, 0, err
	}
	return dnsAnswers(body, qtype)
}

// dnsQuery builds a DNS query message (ID 0, recursion desired).
func dnsQuery(host string, qtype uint16) ([]byte, error) {
	if host == "" || len(host) > 253 {
		return nil, errors.New("bad host name")
	}
	b := []byte{0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return nil, errors.New("bad host name")
		}
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0)
	b = binary.BigEndian.AppendUint16(b, qtype)
	b = binary.BigEndian.AppendUint16(b, 1) // class IN
	return b, nil
}

var errDNSFormat = errors.New("malformed DNS answer")

// skipName advances past a (possibly compressed) domain name.
func skipName(m []byte, off int) (int, error) {
	for i := 0; i < 128; i++ {
		if off >= len(m) {
			return 0, errDNSFormat
		}
		l := int(m[off])
		switch {
		case l == 0:
			return off + 1, nil
		case l&0xC0 == 0xC0:
			if off+1 >= len(m) {
				return 0, errDNSFormat
			}
			return off + 2, nil
		default:
			off += 1 + l
		}
	}
	return 0, errDNSFormat
}

// dnsAnswers extracts the records of qtype from a DNS response and the
// smallest TTL among them. NXDOMAIN and empty answers return no addresses
// and no error (the name simply has no such record).
func dnsAnswers(m []byte, qtype uint16) ([]netip.Addr, uint32, error) {
	if len(m) < 12 {
		return nil, 0, errDNSFormat
	}
	rcode := m[3] & 0x0F
	if rcode != 0 && rcode != 3 { // 3 = NXDOMAIN
		return nil, 0, fmt.Errorf("DNS server error (rcode %d)", rcode)
	}
	qd, an := int(binary.BigEndian.Uint16(m[4:])), int(binary.BigEndian.Uint16(m[6:]))
	off := 12
	var err error
	for i := 0; i < qd; i++ {
		if off, err = skipName(m, off); err != nil {
			return nil, 0, err
		}
		off += 4
	}
	var out []netip.Addr
	ttl := uint32(300)
	for i := 0; i < an; i++ {
		if off, err = skipName(m, off); err != nil {
			return nil, 0, err
		}
		if off+10 > len(m) {
			return nil, 0, errDNSFormat
		}
		typ := binary.BigEndian.Uint16(m[off:])
		t := binary.BigEndian.Uint32(m[off+4:])
		rdlen := int(binary.BigEndian.Uint16(m[off+8:]))
		off += 10
		if off+rdlen > len(m) {
			return nil, 0, errDNSFormat
		}
		if typ == qtype && (rdlen == 4 || rdlen == 16) {
			if a, ok := netip.AddrFromSlice(m[off : off+rdlen]); ok {
				out = append(out, a.Unmap())
				if t < ttl {
					ttl = t
				}
			}
		}
		off += rdlen
	}
	return out, ttl, nil
}
