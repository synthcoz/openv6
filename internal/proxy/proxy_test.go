package proxy

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func setup(t *testing.T, cfg Config, allow ...string) (*Manager, string) {
	t.Helper()
	m := NewManager()
	var ps []netip.Prefix
	for _, a := range allow {
		ps = append(ps, netip.MustParsePrefix(a))
	}
	m.SetAllow(ps)
	cfg.Port = freePort(t)
	cfg.OutIP = netip.MustParseAddr("127.0.0.1")
	if errs := m.Apply([]Config{cfg}); len(errs) > 0 {
		t.Fatal(errs)
	}
	t.Cleanup(m.Close)
	return m, fmt.Sprintf("127.0.0.1:%d", cfg.Port)
}

func target(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s", r.RemoteAddr)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestHTTPForwardWithAuth(t *testing.T) {
	ts := target(t)
	m, addr := setup(t, Config{ID: 1, Protocol: "both", User: "u", Pass: "p"})
	pu, _ := url.Parse("http://u:p@" + addr)
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: 5 * time.Second}
	for i := 0; i < 2; i++ { // keep-alive reuse
		resp, err := cl.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.HasPrefix(string(b), "hello from 127.0.0.1") {
			t.Fatalf("body %q", b)
		}
	}
	// wrong password
	pu2, _ := url.Parse("http://u:bad@" + addr)
	cl2 := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu2)}}
	resp, err := cl2.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 407 {
		t.Fatalf("want 407 got %d", resp.StatusCode)
	}
	c := m.Counters()[1]
	if c.Down.Load() == 0 || c.Conns.Load() == 0 {
		t.Fatalf("counters not updated: %+v", c)
	}
}

func TestHTTPConnect(t *testing.T) {
	ts := target(t)
	_, addr := setup(t, Config{ID: 2, Protocol: "http"}, "127.0.0.0/8")
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	host := strings.TrimPrefix(ts.URL, "http://")
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
	br := bufio.NewReader(c)
	line, _ := br.ReadString('\n')
	if !strings.Contains(line, "200") {
		t.Fatalf("connect: %q", line)
	}
	br.ReadString('\n')
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host)
	b, _ := io.ReadAll(br)
	if !strings.Contains(string(b), "hello from") {
		t.Fatalf("tunnel body %q", b)
	}
}

func socksConnect(t *testing.T, addr, user, pass, host string, port int) (net.Conn, byte) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" {
		c.Write([]byte{5, 1, 2})
	} else {
		c.Write([]byte{5, 1, 0})
	}
	r := make([]byte, 2)
	if _, err := io.ReadFull(c, r); err != nil {
		t.Fatal(err)
	}
	if r[1] == 0xff {
		return c, 0xff
	}
	if user != "" {
		b := []byte{1, byte(len(user))}
		b = append(b, user...)
		b = append(b, byte(len(pass)))
		b = append(b, pass...)
		c.Write(b)
		io.ReadFull(c, r)
		if r[1] != 0 {
			return c, 0xfe
		}
	}
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = append(req, byte(port>>8), byte(port))
	c.Write(req)
	rep := make([]byte, 10)
	io.ReadFull(c, rep)
	return c, rep[1]
}

func TestSOCKS5(t *testing.T) {
	ts := target(t)
	_, addr := setup(t, Config{ID: 3, Protocol: "both", User: "alice", Pass: "secret"})
	tp := ts.Listener.Addr().(*net.TCPAddr).Port
	c, rep := socksConnect(t, addr, "alice", "secret", "localhost", tp)
	defer c.Close()
	if rep != 0 {
		t.Fatalf("socks rep %d", rep)
	}
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	b, _ := io.ReadAll(c)
	if !strings.Contains(string(b), "hello from 127.0.0.1") {
		t.Fatalf("socks body %q", b)
	}
	c2, rep := socksConnect(t, addr, "alice", "wrong", "localhost", tp)
	c2.Close()
	if rep != 0xfe {
		t.Fatalf("expected auth failure, got %d", rep)
	}
	c3, rep := socksConnect(t, addr, "", "", "localhost", tp)
	c3.Close()
	if rep != 0xff {
		t.Fatalf("expected no acceptable method, got %d", rep)
	}
}

func TestWhitelistBlocksNoAuth(t *testing.T) {
	m := NewManager()
	m.SetAllow([]netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")})
	c := &Config{Scope: "lan"}
	if m.clientAllowed(c, &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 1}) {
		t.Fatal("client outside whitelist accepted")
	}
	if !m.clientAllowed(c, &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 1}) {
		t.Fatal("LAN client rejected")
	}
	if !m.clientAllowed(c, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}) {
		t.Fatal("this machine rejected")
	}
}

func TestReconfigure(t *testing.T) {
	m, addr := setup(t, Config{ID: 5, Protocol: "both", User: "a", Pass: "b"})
	port := strings.Split(addr, ":")[1]
	var p int
	fmt.Sscan(port, &p)
	m.Apply([]Config{{ID: 5, Port: p, Protocol: "both", User: "a", Pass: "c", OutIP: netip.MustParseAddr("127.0.0.1")}})
	ts := target(t)
	c, _ := net.Dial("tcp", addr)
	defer c.Close()
	fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: x\r\nProxy-Authorization: Basic %s\r\n\r\n", ts.URL,
		base64.StdEncoding.EncodeToString([]byte("a:c")))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("after reconfigure: %v %v", err, resp)
	}
	m.Apply(nil)
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Fatal("listener should be closed")
	}
}

func TestScopeLocalAndAny(t *testing.T) {
	ts := target(t)
	// "local" binds to 127.0.0.1 and accepts loopback without a password
	_, addr := setup(t, Config{ID: 10, Protocol: "both", Scope: "local"})
	pu, _ := url.Parse("http://" + addr)
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}, Timeout: 3 * time.Second}
	if resp, err := cl.Get(ts.URL); err != nil || resp.StatusCode != 200 {
		t.Fatalf("local scope: %v", err)
	}
	// "any" without password must not become an open proxy: whitelist applies
	m := NewManager()
	m.SetAllow([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	c := &Config{Scope: "any"}
	if m.clientAllowed(c, &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 1}) {
		t.Fatal("open proxy without password")
	}
	c.User, c.Pass = "a", "b"
	if !m.clientAllowed(c, &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 1}) {
		t.Fatal("any+password should accept internet clients")
	}
	c.Scope = "lan"
	if m.clientAllowed(c, &net.TCPAddr{IP: net.ParseIP("203.0.113.5"), Port: 1}) {
		t.Fatal("lan scope accepted an internet client")
	}
}

func TestClassify(t *testing.T) {
	if Classify(ErrNoIPv6) != "no_ipv6_target" {
		t.Fatal("no ipv6")
	}
	_, err := net.Listen("tcp", "127.0.0.1:1")
	if err == nil {
		t.Skip("running as root")
	}
	if c := Classify(err); c != "permission" {
		t.Fatalf("listen on privileged port: %s", c)
	}
}

func TestLimits(t *testing.T) {
	// 64 KiB/s cap: 256 KiB must take roughly 3s (burst 64 KiB)
	big := strings.Repeat("x", 256*1024)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, big) }))
	defer ts.Close()
	_, addr := setup(t, Config{ID: 20, Protocol: "both", User: "a", Pass: "b", RateKBps: 64, MaxConns: 1})
	host := strings.TrimPrefix(ts.URL, "http://")
	start := time.Now()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", host, host, base64.StdEncoding.EncodeToString([]byte("a:b")))
	br := bufio.NewReader(c)
	br.ReadString('\n')
	br.ReadString('\n')
	// a second connection while the first is open exceeds MaxConns=1
	c2, _ := net.Dial("tcp", addr)
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("second connection should be closed")
	}
	c2.Close()
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host)
	b, _ := io.ReadAll(br)
	el := time.Since(start)
	c.Close()
	if len(b) < 256*1024 {
		t.Fatalf("short body %d", len(b))
	}
	if el < 2500*time.Millisecond {
		t.Fatalf("rate limit not applied: %v", el)
	}
	t.Logf("256 KiB at 64 KiB/s took %v", el)
}

func TestBlockPrivateTargets(t *testing.T) {
	out := netip.MustParseAddr("2a01:4f8::10")
	for _, s := range []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "fd00:ec2::254", "fe80::1", "100.64.1.1", "::ffff:127.0.0.1", "0.0.0.0"} {
		if got, _ := filterTargets(out, []netip.Addr{netip.MustParseAddr(s)}); len(got) != 0 {
			t.Errorf("%s not blocked", s)
		}
	}
	for _, s := range []string{"2606:4700::1111", "1.1.1.1", "2001:4860:4860::8888"} {
		if got, _ := filterTargets(out, []netip.Addr{netip.MustParseAddr(s)}); len(got) != 1 {
			t.Errorf("%s blocked", s)
		}
	}
	// lab setups with a private outbound address are not filtered
	if got, _ := filterTargets(netip.MustParseAddr("127.0.0.1"), []netip.Addr{netip.MustParseAddr("127.0.0.1")}); len(got) != 1 {
		t.Error("loopback lab setup filtered")
	}
}
