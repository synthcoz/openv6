// Package netcfg detects IPv6 prefixes and VPNs and installs the outbound
// addresses used by proxies. The platform specific parts live in linux.go,
// windows.go and darwin.go:
//
//   - Linux: addresses are added as /128 with "nodad preferred_lft 0" (never
//     used as the host's default source); VPN bypass uses policy routing.
//   - Windows: addresses are added with netsh "skipassource=true"; VPN bypass
//     binds each proxy socket to the physical interface (IPV6_UNICAST_IF).
//   - macOS: addresses are ifconfig aliases with "pltime 0"; VPN bypass binds
//     sockets with IPV6_BOUND_IF.
package netcfg

import (
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"openv6/internal/proto"
)

type ifAddr struct {
	Addr  netip.Addr
	Plen  int
	Scope int
	Flags int
	Iface string
}

// /proc/net/if_inet6 address flags (also used for addresses from other sources)
const (
	flagTemporary  = 0x01
	flagDeprecated = 0x20
)

// ula reports fc00::/7 (Tailscale fd7a:115c:a1e0::/48, ZeroTier, home ULAs):
// never usable as an Internet source.
func ula(a netip.Addr) bool { b := a.As16(); return b[0]&0xfe == 0xfc }

func interfacesFrom(addrs []ifAddr, managed map[netip.Addr]bool) []proto.Interface {
	var out []proto.Interface
	byIf := map[string]*proto.Interface{}
	var names []string
	for _, a := range addrs {
		if a.Scope != 0 || a.Iface == "lo" || !a.Addr.IsGlobalUnicast() || ula(a.Addr) || managed[a.Addr] {
			continue
		}
		it := byIf[a.Iface]
		if it == nil {
			it = &proto.Interface{Name: a.Iface}
			byIf[a.Iface] = it
			names = append(names, a.Iface)
		}
		it.Addrs = append(it.Addrs, a.Addr.String())
		if a.Plen >= 1 && a.Plen <= 64 && a.Flags&flagTemporary == 0 {
			p, _ := a.Addr.Prefix(64)
			if s := p.String(); !contains(it.Prefixes, s) {
				// non-deprecated prefixes first
				if a.Flags&flagDeprecated == 0 {
					it.Prefixes = append([]string{s}, it.Prefixes...)
				} else {
					it.Prefixes = append(it.Prefixes, s)
				}
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, *byIf[n])
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ---- applying ----
type installed struct {
	Addrs    map[string]string `json:"addrs"`    // ip -> iface
	Routes   map[string]bool   `json:"routes"`   // cidr installed as local route on lo
	Gateways map[string]string `json:"gateways"` // iface -> last known IPv6 router
}

type Manager struct {
	mu        sync.Mutex
	statePath string
	st        installed
	DryRun    bool // log commands instead of executing (non-Linux / tests)
	Run       func(args ...string) error
}

func NewManager(dataDir string) *Manager {
	m := &Manager{statePath: filepath.Join(dataDir, "netstate.json")}
	if b, err := os.ReadFile(m.statePath); err == nil {
		_ = json.Unmarshal(b, &m.st)
	}
	if m.st.Addrs == nil {
		m.st.Addrs = map[string]string{}
	}
	if m.st.Routes == nil {
		m.st.Routes = map[string]bool{}
	}
	if m.st.Gateways == nil {
		m.st.Gateways = map[string]string{}
	}
	m.Run = defaultRunner
	return m
}

func (m *Manager) managedSet() map[netip.Addr]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := map[netip.Addr]bool{}
	for a := range m.st.Addrs {
		if ip, err := netip.ParseAddr(a); err == nil {
			s[ip] = true
		}
	}
	return s
}

func (m *Manager) save() {
	b, _ := json.MarshalIndent(m.st, "", "  ")
	_ = os.WriteFile(m.statePath, b, 0o600)
}

// Desired describes what should be installed.
type Desired struct {
	Iface  string              // interface for "addr" mode addresses
	Addrs  map[netip.Addr]bool // addresses that need to be on Iface
	Routes map[string]bool     // anyip prefixes
}

// Cleanup removes everything this agent installed.
func (m *Manager) Cleanup() {
	_ = m.ApplyBypass(BypassDesired{})
	m.Apply(Desired{})
}

// Policy routing that keeps proxy traffic on the physical interface even when
// a VPN (Cloudflare WARP, Tailscale exit node, WireGuard, commercial VPNs)
// installs its own default route or policy rules. Linux uses the "weak host"
// model, so binding a source address does not choose the interface: without
// these rules, packets from a proxy's FPT address would go into the tunnel
// and be dropped. Rules are per proxy address (/128), never for the whole /64,
// so the machine's own traffic still follows the VPN as the user intended.
const (
	BypassTable = "6464"
	BypassPref  = "100" // before Tailscale (5210-5270), WireGuard fwmark rules and main (32766)
)

// vpnKind classifies an interface name as a VPN tunnel.
func vpnKind(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasPrefix(n, "cloudflarewarp"), n == "warp", strings.HasPrefix(n, "warp"):
		return "warp"
	case strings.HasPrefix(n, "tailscale"):
		return "tailscale"
	case strings.HasPrefix(n, "nordlynx"), strings.HasPrefix(n, "nordtun"):
		return "nordvpn"
	case strings.HasPrefix(n, "proton"), strings.HasPrefix(n, "pvpn"), strings.HasPrefix(n, "ipv6leakintrf"):
		return "protonvpn"
	case strings.HasPrefix(n, "wg"), strings.Contains(n, "mullvad"):
		return "wireguard"
	case strings.HasPrefix(n, "zt"):
		return "zerotier"
	case strings.HasPrefix(n, "tun"), strings.HasPrefix(n, "tap"), strings.Contains(n, "openvpn"),
		strings.Contains(n, "wintun"), strings.Contains(n, "tap-windows"):
		return "openvpn"
	case strings.HasPrefix(n, "utun"), strings.HasPrefix(n, "ipsec"):
		return "other" // macOS: WARP, Tailscale, WireGuard and most VPNs use utunN
	case strings.HasPrefix(n, "ppp"):
		return "ppp"
	}
	return ""
}

// detectVPNs lists tunnel interfaces via netlink (namespace-aware, unlike
// /sys/class/net which reflects the namespace /sys was mounted in).
func detectVPNs() []proto.VPN {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []proto.VPN
	for _, it := range ifs {
		k := vpnKind(it.Name)
		if k == "" {
			continue
		}
		if k == "other" && !hasRoutableAddr(it) {
			continue // idle system tunnels (macOS creates utun0..3 for itself)
		}
		out = append(out, proto.VPN{Iface: it.Name, Kind: k, Up: it.Flags&net.FlagUp != 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Iface < out[j].Iface })
	return out
}

type defRoute struct {
	Iface   string
	Gateway string
	Metric  uint64
}

// choosePrimary picks the physical interface holding the usable /64:
// the configured one if valid, else the default-route interface if it has a
// prefix, else the first non-VPN interface with a prefix.
func choosePrimary(ifs []proto.Interface, configured, defIface string) string {
	has := map[string]bool{}
	var names []string
	for _, it := range ifs {
		if len(it.Prefixes) > 0 && vpnKind(it.Name) == "" {
			has[it.Name] = true
			names = append(names, it.Name)
		}
	}
	switch {
	case configured != "":
		return configured // respect the user's choice even if it currently has no prefix
	case has[defIface]:
		return defIface
	case len(names) > 0:
		return names[0]
	}
	return ""
}

// BypassDesired describes the policy routing to install.
type BypassDesired struct {
	Enabled bool
	Iface   string
	Gateway string   // router link-local address on Iface
	OnLink  []string // prefixes reachable directly on Iface (the /64)
	Sources []string // /128 per proxy address, or routed CIDRs (anyip)
}

// ---- self-diagnosis ----
// SystemChecks returns the machine diagnosis displayed on the node page.
// Check IDs are translated by the UI (chk_<id>, chkfix_<id>).
func (m *Manager) SystemChecks(st proto.NetState, tuneFailed []string, fwManaged bool) []proto.SystemCheck {
	var c []proto.SystemCheck
	c = append(c, toolCheck(m.DryRun))
	c = append(c, proto.SystemCheck{ID: "root", OK: isAdmin() || m.DryRun})
	c = append(c, proto.SystemCheck{ID: "ipv6_enabled", OK: !st.IPv6Disabled})
	c = append(c, proto.SystemCheck{ID: "ipv6_iface", OK: st.PrimaryIface != "", Detail: st.PrimaryIface})
	pfx := ""
	for _, it := range st.Interfaces {
		if it.Name == st.PrimaryIface && len(it.Prefixes) > 0 {
			pfx = it.Prefixes[0]
		}
	}
	c = append(c, proto.SystemCheck{ID: "ipv6_prefix", OK: pfx != "", Detail: pfx})
	c = append(c, proto.SystemCheck{ID: "ipv6_route", OK: st.Gateway != "", Detail: st.Gateway})

	// VPN situation
	var up []string
	for _, v := range st.VPNs {
		if v.Up {
			up = append(up, v.Kind+" ("+v.Iface+")")
		}
	}
	captured := st.EgressIface != "" && st.PrimaryIface != "" && st.EgressIface != st.PrimaryIface && vpnKind(st.EgressIface) != ""
	switch {
	case len(up) == 0:
		c = append(c, proto.SystemCheck{ID: "vpn_none", OK: true})
	case captured && st.Bypass == "ok":
		c = append(c, proto.SystemCheck{ID: "vpn_bypassed", OK: true, Warn: true, Detail: strings.Join(up, ", ")})
	case captured && st.Bypass == "off":
		c = append(c, proto.SystemCheck{ID: "vpn_captures", OK: false, Detail: strings.Join(up, ", ")})
	case captured:
		c = append(c, proto.SystemCheck{ID: "vpn_bypass_failed", OK: false, Detail: strings.Join(up, ", ") + ": " + st.BypassError})
	default:
		c = append(c, proto.SystemCheck{ID: "vpn_present", OK: true, Warn: true, Detail: strings.Join(up, ", ")})
	}
	c = append(c, proto.SystemCheck{ID: "kernel_tuning", OK: len(tuneFailed) == 0, Warn: true, Detail: strings.Join(tuneFailed, ", ")})
	c = append(c, firewallChecks(fwManaged)...)
	return c
}

// egressByUDP finds the interface the OS would use for an Internet IPv6
// destination: connecting a UDP socket sends no packet but selects a route
// and source address. Works on every platform and respects VPN routes.
func egressByUDP() string {
	c, err := net.DialTimeout("udp6", "[2001:4860:4860::8888]:53", time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	src, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	ifs, _ := net.Interfaces()
	for _, it := range ifs {
		addrs, _ := it.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(src.IP) {
				return it.Name
			}
		}
	}
	return ""
}

// netAddrs lists interface addresses through the portable net package
// (Windows, macOS). Prefix length comes from the on-link prefix the OS reports.
func netAddrs() []ifAddr {
	var out []ifAddr
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, it := range ifs {
		if it.Flags&net.FlagLoopback != 0 || it.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := it.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() != nil {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP)
			if !ok {
				continue
			}
			plen, _ := ipn.Mask.Size()
			scope := 0
			if !ip.IsGlobalUnicast() {
				scope = 0x20
			}
			out = append(out, ifAddr{Addr: ip, Plen: plen, Scope: scope, Iface: it.Name})
		}
	}
	return out
}

// ifIndex returns the interface index for socket binding (0 if unknown).
func IfIndex(name string) int {
	if name == "" {
		return 0
	}
	it, err := net.InterfaceByName(name)
	if err != nil {
		return 0
	}
	return it.Index
}

func hasRoutableAddr(it net.Interface) bool {
	addrs, _ := it.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() && !ipn.IP.IsLoopback() {
			return true
		}
	}
	return false
}

// MissingManaged counts addresses openv6 added that are no longer on their
// interface (the OS dropped them after sleep, a cable/Wi-Fi change or an
// adapter reset). One cheap interface listing; used by the agent to decide
// when to re-apply without waiting for the periodic resync.
func (m *Manager) MissingManaged() int {
	if m.DryRun {
		return 0
	}
	present := map[string]bool{}
	for _, a := range netAddrs() {
		present[a.Iface+"|"+a.Addr.String()] = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for a, ifc := range m.st.Addrs {
		if !present[ifc+"|"+a] {
			n++
		}
	}
	return n
}
