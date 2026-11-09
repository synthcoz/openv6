//go:build windows

package netcfg

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"openv6/internal/proto"
)

// SocketBypass: on Windows each proxy socket is bound to the physical
// interface (IPV6_UNICAST_IF), which overrides a VPN's default route.
const SocketBypass = true

var defaultRunner = runNetsh

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

func runNetsh(args ...string) error {
	out, err := hidden(exec.Command("netsh", args...)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// windowsDefaultRoutes parses "netsh interface ipv6 show route". Column
// values are not localized, only the header is, so we match on "::/0".
func windowsDefaultRoutes() []defRoute {
	out, err := hidden(exec.Command("netsh", "interface", "ipv6", "show", "route")).Output()
	if err != nil {
		return nil
	}
	return parseNetshRoutes(string(out))
}

func parseNetshRoutes(out string) []defRoute {
	var rs []defRoute
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[3] != "::/0" {
			continue
		}
		idx, err := strconv.Atoi(f[4])
		if err != nil {
			continue
		}
		r := defRoute{}
		if it, err := net.InterfaceByIndex(idx); err == nil {
			r.Iface = it.Name
		}
		if gw, err := netip.ParseAddr(f[5]); err == nil {
			r.Gateway = gw.String()
		}
		m, _ := strconv.Atoi(f[2])
		r.Metric = uint64(m)
		if r.Iface != "" {
			rs = append(rs, r)
		}
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Metric < rs[j].Metric })
	return rs
}

func (m *Manager) Detect(configured string) proto.NetState {
	st := proto.NetState{VPNs: detectVPNs(), Bypass: "off"}
	routes := windowsDefaultRoutes()
	for _, r := range routes {
		if vpnKind(r.Iface) == "" {
			st.DefaultIface = r.Iface
			break
		}
	}
	st.HasV6Route = len(routes) > 0
	st.Interfaces = interfacesFrom(netAddrs(), m.managedSet())
	st.PrimaryIface = choosePrimary(st.Interfaces, configured, st.DefaultIface)
	st.EgressIface = egressByUDP()
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

// Apply adds proxy addresses with "skipassource=true" so Windows never uses
// them as the source of the machine's own traffic. store=active: they vanish
// on reboot and the agent re-adds them.
func (m *Manager) Apply(d Desired) (map[netip.Addr]error, []error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	addrErrs := map[netip.Addr]error{}
	var errs []error
	if len(d.Routes) > 0 {
		errs = append(errs, fmt.Errorf("routed prefixes (anyip mode) are not supported on Windows; use a /64 pool"))
	}
	idxOf := func(name string) string { return strconv.Itoa(IfIndex(name)) }
	for a, ifc := range m.st.Addrs {
		ip, _ := netip.ParseAddr(a)
		if !d.Addrs[ip] || ifc != d.Iface {
			_ = m.Run("interface", "ipv6", "delete", "address", "interface="+idxOf(ifc), "address="+a, "store=active")
			delete(m.st.Addrs, a)
		}
	}
	if len(d.Addrs) > 0 && d.Iface == "" {
		errs = append(errs, fmt.Errorf("no IPv6 interface found"))
	} else if len(d.Addrs) > 0 {
		present := map[netip.Addr]bool{}
		for _, a := range netAddrs() {
			if a.Iface == d.Iface {
				present[a.Addr] = true
			}
		}
		idx := idxOf(d.Iface)
		added := 0
		for ip := range d.Addrs {
			s := ip.String()
			if _, ok := m.st.Addrs[s]; ok && (present[ip] || m.DryRun) {
				continue
			}
			if present[ip] { // added earlier (e.g. by a previous run): adopt it
				m.st.Addrs[s] = d.Iface
				continue
			}
			if err := m.Run("interface", "ipv6", "add", "address", "interface="+idx, "address="+s+"/128", "store=active", "skipassource=true"); err != nil {
				addrErrs[ip] = err
				continue
			}
			m.st.Addrs[s] = d.Iface
			added++
		}
		// new addresses run duplicate address detection (~1s) before use
		if added > 0 {
			time.Sleep(1200 * time.Millisecond)
		}
	}
	m.save()
	return addrErrs, errs
}

// ApplyBypass is a no-op: proxies bind their sockets to the interface.
func (m *Manager) ApplyBypass(d BypassDesired) error { return nil }

func TuneKernel() []string { return nil }

func UFWActive() bool { return false }

func toolCheck(dry bool) proto.SystemCheck {
	_, err := exec.LookPath("netsh")
	return proto.SystemCheck{ID: "netsh", OK: err == nil || dry}
}

func isAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }

// SyncFirewall makes sure Windows Defender Firewall lets LAN devices reach
// the proxies: one rule for the openv6 program on private/domain networks.
// Public networks stay blocked on purpose (cafés, hotels).
func (m *Manager) SyncFirewall(ports []int) error {
	if m.DryRun || len(ports) == 0 {
		return nil
	}
	fwMu.Lock()
	defer fwMu.Unlock()
	fwOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			fwErr = err
			return
		}
		if hidden(exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name=openv6")).Run() == nil {
			return
		}
		fwErr = runNetsh("advfirewall", "firewall", "add", "rule", "name=openv6", "dir=in", "action=allow",
			"program="+exe, "enable=yes", "profile=private,domain")
	})
	return fwErr
}

var (
	fwOnce sync.Once
	fwMu   sync.Mutex
	fwErr  error

	profMu    sync.Mutex
	profAt    time.Time
	profCache []string
)

// networkCategories returns the categories of connected networks
// (Public/Private/DomainAuthenticated), cached for a minute.
// PowerShell takes seconds to start, so it is refreshed in the background
// and callers never wait for it.
func networkCategories() []string {
	profMu.Lock()
	defer profMu.Unlock()
	if time.Since(profAt) >= time.Minute {
		profAt = time.Now()
		go func() {
			out, err := hidden(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
				"(Get-NetConnectionProfile | Where-Object { $_.IPv6Connectivity -ne 'Disconnected' -or $_.IPv4Connectivity -ne 'Disconnected' }).NetworkCategory")).Output()
			if err != nil {
				return
			}
			profMu.Lock()
			profCache = strings.Fields(string(out))
			profMu.Unlock()
		}()
	}
	return profCache
}

func firewallChecks(fwManaged bool) []proto.SystemCheck {
	var c []proto.SystemCheck
	cats := networkCategories()
	pub := false
	for _, x := range cats {
		if strings.EqualFold(x, "Public") {
			pub = true
		}
	}
	c = append(c, proto.SystemCheck{ID: "network_private", OK: !pub, Warn: true, Detail: strings.Join(cats, ", ")})
	fwMu.Lock()
	ferr := fwErr
	fwMu.Unlock()
	fw := proto.SystemCheck{ID: "firewall", OK: ferr == nil, Warn: true, Detail: "Windows Defender Firewall"}
	if ferr != nil {
		fw.Detail = ferr.Error()
	}
	c = append(c, fw)
	return c
}

// Fix repairs a failing system check (id as in SystemChecks).
func (m *Manager) Fix(id string) (string, error) {
	switch id {
	case "firewall":
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		_ = runNetsh("advfirewall", "firewall", "delete", "rule", "name=openv6")
		err = runNetsh("advfirewall", "firewall", "add", "rule", "name=openv6", "dir=in", "action=allow",
			"program="+exe, "enable=yes", "profile=private,domain")
		fwMu.Lock()
		fwErr = err
		fwMu.Unlock()
		if err != nil {
			return "", err
		}
		return "firewall rule re-created", nil
	case "network_private":
		out, err := hidden(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			"Get-NetConnectionProfile | Where-Object { $_.NetworkCategory -eq 'Public' } | Set-NetConnectionProfile -NetworkCategory Private")).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		profMu.Lock()
		profAt = time.Time{}
		profMu.Unlock()
		return "network set to Private", nil
	}
	return "", fmt.Errorf("no automatic fix for %q", id)
}
