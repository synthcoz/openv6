package proxy

import (
	"errors"
	"net/netip"
	"strings"
)

// ErrBlockedByRule: the destination is on the dashboard's blocked-sites list.
var ErrBlockedByRule = errors.New("this site is blocked on this proxy (Settings → Website access)")

// Blocklist matches destinations against the dashboard's blocked-sites list.
//
// One rule per entry:
//
//	example.com     example.com and every subdomain (www.example.com, a.b.example.com)
//	*.example.com   subdomains only, not example.com itself
//	203.0.113.7     one IP address
//	2001:db8::/32   an IP range
//
// Entries may also be pasted as URLs ("https://www.example.com/path"); the
// host is taken from them. Lines starting with # are comments.
type Blocklist struct {
	domains map[string]bool // domain and its subdomains
	subs    map[string]bool // subdomains only
	nets    []netip.Prefix
}

// ParseBlocklist compiles rules; it returns nil when there is nothing to block.
func ParseBlocklist(rules []string) *Blocklist {
	b := &Blocklist{domains: map[string]bool{}, subs: map[string]bool{}}
	for _, r := range rules {
		r = normalizeRule(r)
		if r == "" {
			continue
		}
		if p, err := netip.ParsePrefix(r); err == nil {
			b.nets = append(b.nets, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(r); err == nil {
			a = a.Unmap()
			b.nets = append(b.nets, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		if strings.HasPrefix(r, "*.") {
			b.subs[r[2:]] = true
		} else {
			b.domains[r] = true
		}
	}
	if len(b.domains)+len(b.subs)+len(b.nets) == 0 {
		return nil
	}
	return b
}

// NormalizeRule cleans one user-entered rule ("" when it is not a rule).
func NormalizeRule(r string) string { return normalizeRule(r) }

func normalizeRule(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	if r == "" || strings.HasPrefix(r, "#") {
		return ""
	}
	if i := strings.Index(r, "://"); i >= 0 { // pasted URL
		r = r[i+3:]
	}
	if i := strings.IndexAny(r, "?#"); i >= 0 {
		r = r[:i]
	}
	if _, err := netip.ParsePrefix(r); err == nil { // CIDR
		return r
	}
	if i := strings.Index(r, "/"); i >= 0 { // URL path
		r = r[:i]
	}
	switch {
	case strings.HasPrefix(r, "["): // [2001:db8::1]:443
		if j := strings.Index(r, "]"); j > 0 {
			r = r[1:j]
		}
	case strings.Count(r, ":") == 1: // host:port
		r = r[:strings.Index(r, ":")]
	}
	r = strings.TrimSuffix(r, ".")
	if _, err := netip.ParseAddr(r); err == nil {
		return r
	}
	if r == "" || strings.ContainsAny(r, " \t,;:") || !strings.Contains(r, ".") {
		return ""
	}
	return r
}

// Host reports whether a hostname (or IP literal) is blocked.
func (b *Blocklist) Host(host string) bool {
	if b == nil {
		return false
	}
	h := strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if a, err := netip.ParseAddr(h); err == nil {
		return b.Addr(a)
	}
	for first := true; h != ""; first = false {
		if b.domains[h] || (!first && b.subs[h]) {
			return true
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			break
		}
		h = h[i+1:]
	}
	return false
}

// Addr reports whether an IP address is blocked.
func (b *Blocklist) Addr(a netip.Addr) bool {
	if b == nil {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range b.nets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
