package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// SiteReport explains why a website does or does not open through a proxy.
type SiteReport struct {
	Host      string   `json:"host"`
	Verdict   string   `json:"verdict"` // ok | blocked_rule | no_dns | ipv4_only | dns_hides_ipv6 | ipv6_unreachable | ipv6_forbidden | ipv6_tls
	SysA      []string `json:"sys_a"`   // what the proxy's normal resolver returned
	SysAAAA   []string `json:"sys_aaaa"`
	SysErr    string   `json:"sys_err,omitempty"`
	DoHA      []string `json:"doh_a"` // what encrypted DNS returned
	DoHAAAA   []string `json:"doh_aaaa"`
	DoHErr    string   `json:"doh_err,omitempty"`
	Tried     string   `json:"tried,omitempty"`   // IPv6 address the test connected to
	Connect   string   `json:"connect,omitempty"` // "ok" or the error
	HTTP      int      `json:"http,omitempty"`    // status code over IPv6 (HTTPS HEAD /)
	Detail    string   `json:"detail,omitempty"`
	SecureDNS bool     `json:"secure_dns"` // encrypted DNS fallback is enabled on this proxy
	Fallback  bool     `json:"ipv4_fallback"`
	Ms        int      `json:"ms"`
}

func strs(a []netip.Addr) []string {
	out := []string{}
	for _, x := range a {
		out = append(out, x.String())
	}
	return out
}

// Diagnose tests one website the way the proxy would reach it, from the
// proxy's own IPv6: blocked list, normal DNS vs encrypted DNS, an IPv6 TCP
// connection on port 443 and an HTTPS HEAD request.
func Diagnose(ctx context.Context, cfg *Config, sys *net.Resolver, doh *DoH, host string) SiteReport {
	start := time.Now()
	h := NormalizeRule(host)
	if strings.HasPrefix(h, "*.") {
		h = h[2:]
	}
	r := SiteReport{Host: h, SecureDNS: cfg.DoH != nil, Fallback: cfg.IPv4Fallback, SysA: []string{}, SysAAAA: []string{}, DoHA: []string{}, DoHAAAA: []string{}}
	defer func() { r.Ms = int(time.Since(start).Milliseconds()) }()
	if strings.Contains(h, "%") {
		h = ""
	}
	if h == "" {
		r.Verdict, r.Detail = "no_dns", "not a valid host name"
		return r
	}
	if cfg.Block.Host(h) {
		r.Verdict = "blocked_rule"
		return r
	}
	if cfg.Resolver != nil {
		sys = cfg.Resolver
	}
	lctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	ips, err := sys.LookupNetIP(lctx, "ip", h)
	cancel()
	if err != nil {
		r.SysErr = err.Error()
	}
	var v6 []netip.Addr
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.Is4() {
			r.SysA = append(r.SysA, ip.String())
		} else {
			r.SysAAAA = append(r.SysAAAA, ip.String())
			v6 = append(v6, ip)
		}
	}
	if doh != nil {
		dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		a4, err4 := doh.Lookup(dctx, h, TypeA, cfg.OutIP, cfg.BindIf)
		a6, err6 := doh.Lookup(dctx, h, TypeAAAA, cfg.OutIP, cfg.BindIf)
		cancel()
		r.DoHA, r.DoHAAAA = strs(a4), strs(a6)
		if err4 != nil && err6 != nil {
			r.DoHErr = err6.Error()
		}
		if len(v6) == 0 {
			v6 = a6
		}
	}
	switch {
	case len(r.SysA)+len(r.SysAAAA)+len(r.DoHA)+len(r.DoHAAAA) == 0:
		r.Verdict = "no_dns"
		return r
	case len(v6) == 0:
		r.Verdict = "ipv4_only"
		return r
	}
	for _, a := range v6 {
		if cfg.blockedAddr(a) {
			r.Verdict = "blocked_rule"
			return r
		}
	}
	// the same destination rules as real proxy traffic: no loopback, LAN,
	// link-local or metadata targets (directly or through NAT64)
	if v6, _ = cfg.filterTargets(v6); len(v6) == 0 {
		r.Verdict, r.Detail = "not_public", "the site resolves only to local or private addresses"
		return r
	}
	// connect over IPv6 from the proxy's address
	target := v6[0]
	r.Tried = target.String()
	d := cfg.Dialer(&net.TCPAddr{IP: cfg.OutIP.AsSlice()})
	d.Timeout = 8 * time.Second
	c, err := d.DialContext(ctx, "tcp6", netip.AddrPortFrom(target, 443).String())
	if err != nil {
		r.Connect, r.Verdict = err.Error(), "ipv6_unreachable"
		return r
	}
	defer c.Close()
	r.Connect = "ok"
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	tc := tls.Client(c, &tls.Config{ServerName: h, MinVersion: tls.VersionTLS12})
	if err := tc.HandshakeContext(ctx); err != nil {
		r.Verdict, r.Detail = "ipv6_tls", err.Error()
		return r
	}
	fmt.Fprintf(tc, "HEAD / HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36\r\nAccept: */*\r\nConnection: close\r\n\r\n", h)
	res, err := http.ReadResponse(bufio.NewReader(tc), nil)
	if err != nil {
		r.Verdict, r.Detail = "ipv6_tls", err.Error()
		return r
	}
	res.Body.Close()
	r.HTTP = res.StatusCode
	r.Verdict = "ok"
	if res.StatusCode == http.StatusForbidden {
		r.Verdict = "ipv6_forbidden"
	}
	if len(r.SysAAAA) == 0 && r.Verdict == "ok" {
		r.Verdict = "dns_hides_ipv6" // works, but only thanks to encrypted DNS
	}
	return r
}
