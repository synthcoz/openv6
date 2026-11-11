package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func TestNormalizeRule(t *testing.T) {
	cases := map[string]string{
		"Example.COM":                     "example.com",
		"https://www.example.com/a?b=1#c": "www.example.com",
		"example.com:443":                 "example.com",
		"*.ads.example.com":               "*.ads.example.com",
		"203.0.113.7":                     "203.0.113.7",
		"2001:db8::/32":                   "2001:db8::/32",
		"[2001:db8::1]:443":               "2001:db8::1",
		"http://[2001:db8::1]/x":          "2001:db8::1",
		"example.com.":                    "example.com",
		"# comment":                       "",
		"localhost":                       "",
		"not a site":                      "",
		"":                                "",
	}
	for in, want := range cases {
		if got := NormalizeRule(in); got != want {
			t.Errorf("NormalizeRule(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlocklistMatch(t *testing.T) {
	b := ParseBlocklist([]string{"facebook.com", "*.ads.example.com", "203.0.113.0/24", "2001:db8::1", "junk rule"})
	block := []string{"facebook.com", "www.facebook.com", "a.b.facebook.com", "FACEBOOK.COM.", "x.ads.example.com", "203.0.113.9", "[2001:db8::1]"}
	allow := []string{"notfacebook.com", "facebook.com.evil.net", "ads.example.com", "example.com", "203.0.114.1", "2001:db8::2"}
	for _, h := range block {
		if !b.Host(h) {
			t.Errorf("%s should be blocked", h)
		}
	}
	for _, h := range allow {
		if b.Host(h) {
			t.Errorf("%s should not be blocked", h)
		}
	}
	if ParseBlocklist([]string{"", "# only comments"}) != nil {
		t.Fatal("empty list should be nil")
	}
	var none *Blocklist
	if none.Host("example.com") || none.Addr(netip.MustParseAddr("1.2.3.4")) {
		t.Fatal("nil blocklist blocks nothing")
	}
}

func TestBlockedSiteHTTPAndSOCKS(t *testing.T) {
	ts := target(t)
	_, addr := setup(t, Config{ID: 1, Protocol: "both", Block: ParseBlocklist([]string{"127.0.0.1"})}, "127.0.0.0/8")
	// HTTP CONNECT -> 403
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", ts.Listener.Addr(), ts.Listener.Addr())
	res, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("CONNECT to a blocked site: want 403, got %d", res.StatusCode)
	}
	// SOCKS5 -> reply 0x02 (not allowed by ruleset)
	s, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	s.Write([]byte{5, 1, 0})
	g := make([]byte, 2)
	if _, err := s.Read(g); err != nil {
		t.Fatal(err)
	}
	ap := netip.MustParseAddrPort(ts.Listener.Addr().String())
	req := []byte{5, 1, 0, 1}
	req = append(req, ap.Addr().AsSlice()...)
	req = binary.BigEndian.AppendUint16(req, ap.Port())
	s.Write(req)
	rep := make([]byte, 10)
	if _, err := s.Read(rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != repNotAllow {
		t.Fatalf("SOCKS5 to a blocked site: want reply 2, got %d", rep[1])
	}
	if Classify(ErrBlockedByRule) != "blocked_rule" {
		t.Fatal("classify")
	}
}

// dnsResponse builds a DNS answer for tests: one question, the given records
// (each with a compressed name pointer), optional CNAME first.
func dnsResponse(qtype uint16, cname bool, addrs ...netip.Addr) []byte {
	q, _ := dnsQuery("example.com", qtype)
	m := append([]byte{}, q...)
	m[2], m[3] = 0x81, 0x80 // response, RD, RA
	an := len(addrs)
	if cname {
		an++
	}
	binary.BigEndian.PutUint16(m[6:], uint16(an))
	if cname {
		m = append(m, 0xC0, 12) // name -> question
		m = binary.BigEndian.AppendUint16(m, 5)
		m = binary.BigEndian.AppendUint16(m, 1)
		m = binary.BigEndian.AppendUint32(m, 60)
		target := []byte{3, 'w', 'w', 'w', 0xC0, 12}
		m = binary.BigEndian.AppendUint16(m, uint16(len(target)))
		m = append(m, target...)
	}
	for _, a := range addrs {
		m = append(m, 0xC0, 12)
		m = binary.BigEndian.AppendUint16(m, qtype)
		m = binary.BigEndian.AppendUint16(m, 1)
		m = binary.BigEndian.AppendUint32(m, 120)
		b := a.AsSlice()
		m = binary.BigEndian.AppendUint16(m, uint16(len(b)))
		m = append(m, b...)
	}
	return m
}

func TestDNSWireFormat(t *testing.T) {
	q, err := dnsQuery("www.example.com", TypeAAAA)
	if err != nil || len(q) != 12+17+4 || q[5] != 1 || q[2] != 1 {
		t.Fatalf("query %x %v", q, err)
	}
	if _, err := dnsQuery("bad..name", TypeA); err == nil {
		t.Fatal("empty label accepted")
	}
	a6 := netip.MustParseAddr("2001:db8::7")
	got, ttl, err := dnsAnswers(dnsResponse(TypeAAAA, true, a6), TypeAAAA)
	if err != nil || len(got) != 1 || got[0] != a6 || ttl != 120 {
		t.Fatalf("answers %v ttl %d err %v", got, ttl, err)
	}
	a4 := netip.MustParseAddr("203.0.113.5")
	got, _, err = dnsAnswers(dnsResponse(TypeA, false, a4), TypeA)
	if err != nil || len(got) != 1 || got[0] != a4 {
		t.Fatalf("A answers %v %v", got, err)
	}
	if _, _, err := dnsAnswers([]byte{1, 2, 3}, TypeA); err == nil {
		t.Fatal("short message accepted")
	}
	nx := dnsResponse(TypeAAAA, false)
	nx[3] |= 3 // NXDOMAIN: no addresses, no error
	if got, _, err := dnsAnswers(nx, TypeAAAA); err != nil || len(got) != 0 {
		t.Fatalf("nxdomain %v %v", got, err)
	}
	trunc := dnsResponse(TypeAAAA, false, a6)
	if _, _, err := dnsAnswers(trunc[:len(trunc)-3], TypeAAAA); err == nil {
		t.Fatal("truncated record accepted")
	}
}

// The proxy asks encrypted DNS for an IPv6 address only when the normal
// resolver has none; the answer is cached, so a pre-filled cache stands in
// for the network here.
func TestResolveFallsBackToDoH(t *testing.T) {
	m := NewManager()
	d := NewDoH()
	v6 := netip.MustParseAddr("2001:db8::99")
	d.cache.Store(fmt.Sprintf("%d|%s", TypeAAAA, "site.invalid"), dohEntry{addrs: []netip.Addr{v6}, exp: time.Now().Add(time.Minute)})
	cfg := &Config{OutIP: netip.MustParseAddr("2001:db8::1"), DoH: d,
		Resolver: &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return nil, fmt.Errorf("no DNS in this test")
		}}}
	got, err := m.resolve(context.Background(), cfg, "site.invalid")
	if err != nil || len(got) == 0 || got[0] != v6 {
		t.Fatalf("want DoH answer %s, got %v %v", v6, got, err)
	}
	cfg.DoH = nil // disabled: the resolver error comes back
	if _, err := m.resolve(context.Background(), cfg, "site.invalid"); err == nil {
		t.Fatal("expected the resolver error without DoH")
	}
}

func TestNAT64Addr(t *testing.T) {
	got := nat64Addr(netip.MustParsePrefix("64:ff9b::/96"), netip.MustParseAddr("192.0.2.33"))
	if got != netip.MustParseAddr("64:ff9b::c000:221") {
		t.Fatalf("well-known prefix: %s", got)
	}
	got = nat64Addr(netip.MustParsePrefix("2001:db8:64:64::/96"), netip.MustParseAddr("203.0.113.5"))
	if got != netip.MustParseAddr("2001:db8:64:64::cb00:7105") {
		t.Fatalf("custom prefix: %s", got)
	}
}

// An IPv6 proxy reaches an IPv4-only site only when the site is on the
// IPv4 list (or fallback is on), and the connection log says how it left.
func TestIPv4SitesAndConnLog(t *testing.T) {
	ts := target(t)
	ap := netip.MustParseAddrPort(ts.Listener.Addr().String())
	m := NewManager()
	m.SetAllow([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	cfg := Config{ID: 7, Protocol: "both", User: "u", Pass: "p", Port: freePort(t), OutIP: netip.MustParseAddr("::1")}
	if errs := m.Apply([]Config{cfg}); len(errs) > 0 {
		t.Fatal(errs)
	}
	t.Cleanup(m.Close)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	get := func(user string) int {
		pu, _ := url.Parse("http://" + user + "@" + addr)
		cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: 5 * time.Second}
		res, err := cl.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get("u:p"); code != http.StatusBadGateway {
		t.Fatalf("IPv4-only target without exception: want 502, got %d", code)
	}
	cfg.IPv4Sites = ParseBlocklist([]string{ap.Addr().String()})
	m.Apply([]Config{cfg})
	if code := get("u:p"); code != 200 {
		t.Fatalf("IPv4 exception: want 200, got %d", code)
	}
	get("u:wrong")
	log := m.ConnLog(7)
	if len(log) < 3 || log[0].Result != "auth" || log[1].Result != "ok" || log[1].Via != "ipv4" || log[2].Result != "no_ipv6_target" {
		t.Fatalf("connection log %+v", log)
	}
	if log[1].Client != "127.0.0.1" || log[1].Proto != "http" || log[1].Target != ap.String() {
		t.Fatalf("event fields %+v", log[1])
	}
	// outside the sharing scope: the refusal is logged with the source
	l := m.listeners[cfg.Port]
	if m.clientAllowed(l.cfg.Load(), &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 1}) {
		t.Fatal("public source allowed on a LAN proxy")
	}
	var cl connLog
	for i := 0; i < connLogSize+5; i++ {
		cl.add(ConnEvent{Result: fmt.Sprint(i)})
	}
	if got := cl.list(); len(got) != connLogSize || got[0].Result != fmt.Sprint(connLogSize+4) {
		t.Fatalf("ring buffer: %d events, newest %s", len(got), got[0].Result)
	}
}
