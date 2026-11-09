// Package netcfg detects IPv6 prefixes and installs outbound addresses on Linux.
//
// Two modes per pool:
//   - "addr":  every proxy IP is added to the interface as a /128 with
//     "nodad preferred_lft 0". The kernel answers Neighbor Solicitations for it,
//     so this works on home networks where the /64 is on-link (no ndppd needed).
//     preferred_lft 0 marks the address deprecated so the host never picks it as
//     the default source for its own traffic; explicit bind still works.
//   - "anyip": a routed prefix (VPS /48, /56, or a prefix delegated by the home
//     router to this box) is installed as a local route on lo and
//     net.ipv6.ip_nonlocal_bind=1 lets the proxy bind any address in it.
package netcfg

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"openv6/internal/proto"
)

const (
	flagTemporary  = 0x01
	flagDeprecated = 0x20
)

type ifAddr struct {
	Addr  netip.Addr
	Plen  int
	Scope int
	Flags int
	Iface string
}

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

func defaultV6Iface() string {
	f, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		// dest dest_plen src src_plen nexthop metric refcnt use flags iface
		if len(fs) >= 10 && fs[0] == strings.Repeat("0", 32) && fs[1] == "00" && fs[9] != "lo" {
			return fs[9]
		}
	}
	return ""
}

// Detect returns the node's IPv6 network state. "managed" addresses (added by
// this agent) are excluded so they are never mistaken for the base prefix.
func (m *Manager) Detect() proto.NetState {
	st := proto.NetState{}
	st.DefaultIface = defaultV6Iface()
	st.HasV6Route = st.DefaultIface != ""
	addrs, err := readIfInet6()
	if err != nil {
		return st
	}
	st.Interfaces = interfacesFrom(addrs, m.managedSet())
	return st
}

func interfacesFrom(addrs []ifAddr, managed map[netip.Addr]bool) []proto.Interface {
	var out []proto.Interface
	byIf := map[string]*proto.Interface{}
	var names []string
	for _, a := range addrs {
		if a.Scope != 0 || a.Iface == "lo" || !a.Addr.IsGlobalUnicast() || managed[a.Addr] {
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
	Addrs  map[string]string `json:"addrs"`  // ip -> iface
	Routes map[string]bool   `json:"routes"` // cidr installed as local route on lo
}

type Manager struct {
	mu        sync.Mutex
	statePath string
	st        installed
	DryRun    bool // log commands instead of executing (non-Linux / tests)
	Run       func(args ...string) error
}

func NewManager(dataDir string) *Manager {
	m := &Manager{statePath: filepath.Join(dataDir, "netstate.json"), st: installed{Addrs: map[string]string{}, Routes: map[string]bool{}}}
	if b, err := os.ReadFile(m.statePath); err == nil {
		_ = json.Unmarshal(b, &m.st)
		if m.st.Addrs == nil {
			m.st.Addrs = map[string]string{}
		}
		if m.st.Routes == nil {
			m.st.Routes = map[string]bool{}
		}
	}
	m.Run = runIP
	return m
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

// Cleanup removes everything this agent installed.
func (m *Manager) Cleanup() {
	m.Apply(Desired{})
}
