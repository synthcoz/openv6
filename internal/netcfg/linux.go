//go:build linux

package netcfg

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"openv6/internal/proto"
)

func readIfInet6() ([]ifAddr, error) {
	f, err := os.Open("/proc/net/if_inet6")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseIfInet6(f)
}

func parseIfInet6(r io.Reader) ([]ifAddr, error) {
	var out []ifAddr
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 6 {
			continue
		}
		b, err := hex.DecodeString(fs[0])
		if err != nil || len(b) != 16 {
			continue
		}
		plen, _ := strconv.ParseInt(fs[2], 16, 32)
		scope, _ := strconv.ParseInt(fs[3], 16, 32)
		flags, _ := strconv.ParseInt(fs[4], 16, 32)
		out = append(out, ifAddr{Addr: netip.AddrFrom16([16]byte(b)), Plen: int(plen), Scope: int(scope), Flags: int(flags), Iface: fs[5]})
	}
	return out, sc.Err()
}

// Detect returns the node's IPv6 network state. "managed" addresses (added by
// this agent) are excluded so they are never mistaken for the base prefix.
// configured is the interface chosen in the panel ("" = automatic).
func (m *Manager) Detect(configured string) proto.NetState {
	st := proto.NetState{VPNs: detectVPNs(), Bypass: "off"}
	routes := defaultRoutes()
	for _, r := range routes {
		if vpnKind(r.Iface) == "" {
			st.DefaultIface = r.Iface
			break
		}
	}
	if st.DefaultIface == "" && len(routes) > 0 {
		st.DefaultIface = routes[0].Iface
	}
	st.HasV6Route = len(routes) > 0
	if addrs, err := readIfInet6(); err == nil {
		st.Interfaces = interfacesFrom(addrs, m.managedSet())
	}
	st.PrimaryIface = choosePrimary(st.Interfaces, configured, st.DefaultIface)
	st.IPv6Disabled = ipv6Disabled(st.PrimaryIface)
	st.EgressIface = egressIface()
	// router of the primary interface; remembered because some VPNs remove
	// the physical default route while they are connected
	m.mu.Lock()
	for _, r := range routes {
		if r.Iface == st.PrimaryIface && r.Gateway != "" {
			st.Gateway = r.Gateway
			if m.st.Gateways[r.Iface] != r.Gateway {
				m.st.Gateways[r.Iface] = r.Gateway
				m.save()
			}
			break
		}
	}
	if st.Gateway == "" {
		st.Gateway = m.st.Gateways[st.PrimaryIface]
	}
	m.mu.Unlock()
	if st.PrimaryIface != "" && st.Gateway != "" {
		st.HasV6Route = true
	}
	return st
}

func runIP(args ...string) error {
	if _, err := exec.LookPath("ip"); err != nil {
		return fmt.Errorf("command 'ip' not found: install iproute2 (apt install iproute2)")
	}
	out, err := exec.Command("ip", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Apply reconciles kernel state. Returns per-address errors and general errors.
func (m *Manager) Apply(d Desired) (map[netip.Addr]error, []error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	addrErrs := map[netip.Addr]error{}
	var errs []error

	if len(d.Routes) > 0 {
		if err := os.WriteFile("/proc/sys/net/ipv6/ip_nonlocal_bind", []byte("1"), 0o644); err != nil && !m.DryRun {
			errs = append(errs, fmt.Errorf("enable ip_nonlocal_bind: %w", err))
		}
	}
	for cidr := range m.st.Routes {
		if !d.Routes[cidr] {
			if err := m.Run("-6", "route", "del", "local", cidr, "dev", "lo"); err != nil {
				errs = append(errs, err)
			}
			delete(m.st.Routes, cidr)
		}
	}
	for cidr := range d.Routes {
		if err := m.Run("-6", "route", "replace", "local", cidr, "dev", "lo"); err != nil {
			errs = append(errs, err)
			continue
		}
		m.st.Routes[cidr] = true
	}

	for a, ifc := range m.st.Addrs {
		ip, _ := netip.ParseAddr(a)
		if !d.Addrs[ip] || ifc != d.Iface {
			_ = m.Run("-6", "addr", "del", a+"/128", "dev", ifc)
			delete(m.st.Addrs, a)
		}
	}
	if len(d.Addrs) > 0 && d.Iface == "" {
		errs = append(errs, fmt.Errorf("no IPv6 interface found"))
	} else {
		// addresses vanish on reboot / link reset; re-add anything not present
		present := map[netip.Addr]bool{}
		if cur, err := readIfInet6(); err == nil {
			for _, a := range cur {
				if a.Iface == d.Iface {
					present[a.Addr] = true
				}
			}
		}
		for ip := range d.Addrs {
			s := ip.String()
			if _, ok := m.st.Addrs[s]; ok && (present[ip] || m.DryRun) {
				continue
			}
			if err := m.Run("-6", "addr", "replace", s+"/128", "dev", d.Iface, "nodad", "valid_lft", "forever", "preferred_lft", "0"); err != nil {
				addrErrs[ip] = err
				continue
			}
			m.st.Addrs[s] = d.Iface
		}
	}
	m.save()
	return addrErrs, errs
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

// sysctlMin lists kernel settings raised (never lowered) for many addresses
// and connections. gc_thresh controls the IPv6 neighbor table: with the default
// (1024 hard limit on some distros) busy proxies start dropping neighbors.
var sysctlMin = map[string]int{
	"net/ipv6/neigh/default/gc_thresh1": 1024,
	"net/ipv6/neigh/default/gc_thresh2": 4096,
	"net/ipv6/neigh/default/gc_thresh3": 8192,
	"net/ipv6/route/max_size":           65536,
	"net/core/somaxconn":                4096,
	"net/ipv4/tcp_max_syn_backlog":      4096,
	"fs/file-max":                       1048576,
}

// TuneKernel raises kernel limits. Returns the settings it could not apply.
func TuneKernel() []string {
	var failed []string
	keys := make([]string, 0, len(sysctlMin))
	for k := range sysctlMin {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := filepath.Join("/proc/sys", k)
		b, err := os.ReadFile(p)
		if err != nil {
			continue // not present on this kernel
		}
		cur, _ := strconv.Atoi(strings.Fields(string(b) + " 0")[0])
		if cur >= sysctlMin[k] {
			continue
		}
		if err := os.WriteFile(p, []byte(strconv.Itoa(sysctlMin[k])), 0o644); err != nil {
			failed = append(failed, strings.ReplaceAll(k, "/", "."))
		}
	}
	return failed
}

// SysctlConf is written to /etc/sysctl.d by "openv6 install" so the values
// survive reboots even before the service starts.
func SysctlConf() string {
	var sb strings.Builder
	sb.WriteString("# written by openv6 install\n")
	keys := make([]string, 0, len(sysctlMin))
	for k := range sysctlMin {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s = %d\n", strings.ReplaceAll(k, "/", "."), sysctlMin[k])
	}
	return sb.String()
}

// ---- firewall (ufw) ----
const ufwComment = "openv6"

// UFWActive reports whether ufw is installed and enabled.
func UFWActive() bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false
	}
	out, err := exec.Command("ufw", "status").CombinedOutput()
	return err == nil && strings.Contains(string(out), "Status: active")
}

// SyncFirewall makes sure exactly the given TCP ports are opened by openv6
// rules (identified by their comment). Other ufw rules are never touched.
func (m *Manager) SyncFirewall(ports []int) error {
	if m.DryRun || !UFWActive() {
		return nil
	}
	out, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw status: %v", err)
	}
	have := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "# "+ufwComment) || strings.Contains(line, "(v6)") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSuffix(f[0], "/tcp")); err == nil {
			have[p] = true
		}
	}
	want := map[int]bool{}
	for _, p := range ports {
		want[p] = true
	}
	var errs []string
	for p := range want {
		if !have[p] {
			if b, err := exec.Command("ufw", "allow", fmt.Sprintf("%d/tcp", p), "comment", ufwComment).CombinedOutput(); err != nil {
				errs = append(errs, strings.TrimSpace(string(b)))
			}
		}
	}
	for p := range have {
		if !want[p] {
			_ = exec.Command("ufw", "--force", "delete", "allow", fmt.Sprintf("%d/tcp", p)).Run()
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("ufw: %s", strings.Join(errs, "; "))
	}
	return nil
}

// defaultRunner executes iproute2.
var defaultRunner = runIP

func toolCheck(dry bool) proto.SystemCheck {
	_, err := exec.LookPath("ip")
	return proto.SystemCheck{ID: "iproute2", OK: err == nil || dry}
}

func isAdmin() bool { return os.Geteuid() == 0 }

func firewallChecks(fwManaged bool) []proto.SystemCheck {
	fw := proto.SystemCheck{ID: "firewall", OK: true, Warn: true, Detail: "inactive"}
	if UFWActive() {
		fw.Detail = "ufw active"
		if fwManaged {
			fw.Detail = "ufw active, ports managed by openv6"
		} else {
			fw.OK = false
		}
	}
	return []proto.SystemCheck{fw}
}

// SocketBypass reports whether proxies must bind sockets to the physical
// interface themselves (false on Linux: policy routing does it).
const SocketBypass = false

// Fix repairs a failing system check (id as in SystemChecks).
func (m *Manager) Fix(id string) (string, error) {
	switch id {
	case "kernel_tuning":
		if failed := TuneKernel(); len(failed) > 0 {
			return "", fmt.Errorf("could not set %s", strings.Join(failed, ", "))
		}
		return "kernel limits raised", nil
	}
	return "", fmt.Errorf("no automatic fix for %q", id)
}
