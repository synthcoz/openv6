package netcfg

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"openv6/internal/proto"
)

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
	case strings.HasPrefix(n, "tun"), strings.HasPrefix(n, "tap"):
		return "openvpn"
	case strings.HasPrefix(n, "ppp"):
		return "ppp"
	}
	return ""
}

func ifaceUp(name string) bool {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "operstate"))
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(b))
	return s == "up" || s == "unknown" // tunnels report "unknown"
}

func detectVPNs() []proto.VPN {
	ents, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	var out []proto.VPN
	for _, e := range ents {
		if k := vpnKind(e.Name()); k != "" {
			out = append(out, proto.VPN{Iface: e.Name(), Kind: k, Up: ifaceUp(e.Name())})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Iface < out[j].Iface })
	return out
}

func ipv6Disabled(iface string) bool {
	read := func(p string) bool {
		b, err := os.ReadFile(p)
		return err == nil && strings.TrimSpace(string(b)) == "1"
	}
	if read("/proc/sys/net/ipv6/conf/all/disable_ipv6") {
		return true
	}
	return iface != "" && read(filepath.Join("/proc/sys/net/ipv6/conf", iface, "disable_ipv6"))
}

type defRoute struct {
	Iface   string
	Gateway string
	Metric  uint64
}

// defaultRoutes parses ::/0 routes from /proc/net/ipv6_route.
func defaultRoutes() []defRoute {
	f, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return nil
	}
	defer f.Close()
	return parseDefaultRoutes(bufio.NewScanner(f))
}

func parseDefaultRoutes(sc *bufio.Scanner) []defRoute {
	var out []defRoute
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		// dest dest_plen src src_plen nexthop metric refcnt use flags iface
		if len(fs) < 10 || fs[0] != strings.Repeat("0", 32) || fs[1] != "00" || fs[9] == "lo" {
			continue
		}
		r := defRoute{Iface: fs[9]}
		if b, err := hex.DecodeString(fs[4]); err == nil && len(b) == 16 {
			if gw := netip.AddrFrom16([16]byte(b)); !gw.IsUnspecified() {
				r.Gateway = gw.String()
			}
		}
		fmt.Sscanf(fs[5], "%x", &r.Metric)
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	return out
}

// egressIface asks the kernel which interface an Internet IPv6 destination
// would use (after policy rules), e.g. CloudflareWARP when WARP is on.
func egressIface() string {
	if _, err := exec.LookPath("ip"); err != nil {
		return ""
	}
	out, err := exec.Command("ip", "-6", "route", "get", "2001:4860:4860::8888").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "dev" {
			return f[i+1]
		}
	}
	return ""
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

// ---- bypass rules ----

// listBypassRules returns the "from" selectors of rules pointing to our table.
func listBypassRules() (map[string]bool, error) {
	out, err := exec.Command("ip", "-6", "rule", "show", "table", BypassTable).Output()
	if err != nil {
		return nil, err
	}
	return parseRuleSelectors(string(out)), nil
}

func parseRuleSelectors(out string) map[string]bool {
	m := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "from" && f[i+1] != "all" {
				sel := f[i+1]
				if !strings.Contains(sel, "/") {
					sel += "/128"
				}
				m[sel] = true
			}
		}
	}
	return m
}

// BypassDesired describes the policy routing to install.
type BypassDesired struct {
	Enabled bool
	Iface   string
	Gateway string   // router link-local address on Iface
	OnLink  []string // prefixes reachable directly on Iface (the /64)
	Sources []string // /128 per proxy address, or routed CIDRs (anyip)
}

// ApplyBypass reconciles rules and the bypass table. It never touches rules
// or tables other than its own.
func (m *Manager) ApplyBypass(d BypassDesired) error {
	if m.DryRun {
		for _, s := range d.Sources {
			_ = m.Run("-6", "rule", "add", "from", s, "lookup", BypassTable, "pref", BypassPref)
		}
		return nil
	}
	if _, err := exec.LookPath("ip"); err != nil {
		return fmt.Errorf("iproute2 missing")
	}
	have, err := listBypassRules()
	if err != nil {
		return fmt.Errorf("ip rule: %v", err)
	}
	want := map[string]bool{}
	if d.Enabled && d.Iface != "" && d.Gateway != "" {
		for _, s := range d.Sources {
			want[s] = true
		}
	}
	for s := range have {
		if !want[s] {
			_ = m.Run("-6", "rule", "del", "from", s, "lookup", BypassTable, "pref", BypassPref)
		}
	}
	if len(want) == 0 {
		_ = m.Run("-6", "route", "flush", "table", BypassTable)
		if d.Enabled && d.Iface != "" && d.Gateway == "" && len(d.Sources) > 0 {
			return fmt.Errorf("no IPv6 router known on %s", d.Iface)
		}
		return nil
	}
	if err := m.Run("-6", "route", "replace", "default", "via", d.Gateway, "dev", d.Iface, "table", BypassTable); err != nil {
		return err
	}
	for _, p := range d.OnLink {
		_ = m.Run("-6", "route", "replace", p, "dev", d.Iface, "table", BypassTable)
	}
	var errs []string
	for s := range want {
		if have[s] {
			continue
		}
		if err := m.Run("-6", "rule", "add", "from", s, "lookup", BypassTable, "pref", BypassPref); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
