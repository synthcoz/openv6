//go:build darwin

package netcfg

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"openv6/internal/proto"
)

// SocketBypass: on macOS each proxy socket is bound to the physical
// interface (IPV6_BOUND_IF), which overrides a VPN's default route.
const SocketBypass = true

var defaultRunner = runIfconfig

func runIfconfig(args ...string) error {
	out, err := exec.Command("ifconfig", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ifconfig %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// darwinDefaultRoute parses `route -n get -inet6 default`.
func darwinDefaultRoute() (iface, gw string) {
	out, err := exec.Command("route", "-n", "get", "-inet6", "default").Output()
	if err != nil {
		return "", ""
	}
	return parseRouteGet(string(out))
}

func parseRouteGet(out string) (iface, gw string) {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "interface":
			iface = v
		case "gateway":
			gw, _, _ = strings.Cut(v, "%")
		}
	}
	return
}

// physicalGateway finds the router of a physical interface even when a VPN
// owns the default route, by asking for the scoped default route.
func physicalGateway(iface string) string {
	out, err := exec.Command("route", "-n", "get", "-inet6", "-ifscope", iface, "default").Output()
	if err != nil {
		return ""
	}
	_, gw := parseRouteGet(string(out))
	return gw
}

func (m *Manager) Detect(configured string) proto.NetState {
	st := proto.NetState{VPNs: detectVPNs(), Bypass: "off"}
	defIf, _ := darwinDefaultRoute()
	st.HasV6Route = defIf != ""
	if vpnKind(defIf) == "" {
		st.DefaultIface = defIf
	}
	st.Interfaces = interfacesFrom(netAddrs(), m.managedSet())
	st.PrimaryIface = choosePrimary(st.Interfaces, configured, st.DefaultIface)
	st.EgressIface = egressByUDP()
	m.mu.Lock()
	if st.PrimaryIface != "" {
		if gw := physicalGateway(st.PrimaryIface); gw != "" {
			st.Gateway = gw
			if m.st.Gateways[st.PrimaryIface] != gw {
				m.st.Gateways[st.PrimaryIface] = gw
				m.save()
			}
		} else {
			st.Gateway = m.st.Gateways[st.PrimaryIface]
		}
	}
	m.mu.Unlock()
	if st.PrimaryIface != "" && st.Gateway != "" {
		st.HasV6Route = true
	}
	return st
}

// Apply adds proxy addresses as interface aliases. "pltime 0" marks them
// deprecated so macOS never picks them as the machine's own source address.
func (m *Manager) Apply(d Desired) (map[netip.Addr]error, []error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	addrErrs := map[netip.Addr]error{}
	var errs []error
	if len(d.Routes) > 0 {
		errs = append(errs, fmt.Errorf("routed prefixes (anyip mode) are not supported on macOS; use a /64 pool"))
	}
	for a, ifc := range m.st.Addrs {
		ip, _ := netip.ParseAddr(a)
		if !d.Addrs[ip] || ifc != d.Iface {
			_ = m.Run(ifc, "inet6", a, "-alias")
			delete(m.st.Addrs, a)
		}
	}
	var added []string
	if len(d.Addrs) > 0 && d.Iface == "" {
		errs = append(errs, fmt.Errorf("no IPv6 interface found"))
	} else {
		present := map[netip.Addr]bool{}
		for _, a := range netAddrs() {
			if a.Iface == d.Iface {
				present[a.Addr] = true
			}
		}
		for ip := range d.Addrs {
			s := ip.String()
			if _, ok := m.st.Addrs[s]; ok && (present[ip] || m.DryRun) {
				continue
			}
			err := m.Run(d.Iface, "inet6", s, "prefixlen", "128", "alias", "pltime", "0")
			if err != nil { // older ifconfig without pltime
				err = m.Run(d.Iface, "inet6", s, "prefixlen", "128", "alias")
			}
			if err != nil {
				addrErrs[ip] = err
				continue
			}
			m.st.Addrs[s] = d.Iface
			added = append(added, s)
		}
	}
	if len(added) > 0 && !m.DryRun {
		waitDAD(d.Iface, added)
	}
	m.save()
	return addrErrs, errs
}

// waitDAD waits (max 4s) until macOS finished duplicate address detection:
// a "tentative" address cannot be used as a source yet.
func waitDAD(iface string, addrs []string) {
	for i := 0; i < 40; i++ {
		out, err := exec.Command("ifconfig", iface, "inet6").Output()
		if err != nil {
			return
		}
		pending := false
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "tentative") {
				continue
			}
			for _, a := range addrs {
				if strings.Contains(line, a+" ") || strings.Contains(line, a+"%") {
					pending = true
				}
			}
		}
		if !pending {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m *Manager) ApplyBypass(d BypassDesired) error { return nil }

func TuneKernel() []string { return nil }

func UFWActive() bool { return false }

func toolCheck(dry bool) proto.SystemCheck {
	_, err := exec.LookPath("ifconfig")
	return proto.SystemCheck{ID: "ifconfig", OK: err == nil || dry}
}

func isAdmin() bool { return os.Geteuid() == 0 }

const socketfilterfw = "/usr/libexec/ApplicationFirewall/socketfilterfw"

var (
	fwOnce sync.Once
	fwErr  error
)

// SyncFirewall allows openv6 through the macOS application firewall when it
// is enabled (otherwise incoming LAN connections are silently dropped).
func (m *Manager) SyncFirewall(ports []int) error {
	if m.DryRun || len(ports) == 0 {
		return nil
	}
	fwOnce.Do(func() {
		out, err := exec.Command(socketfilterfw, "--getglobalstate").Output()
		if err != nil || !strings.Contains(strings.ToLower(string(out)), "enabled") {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			fwErr = err
			return
		}
		_ = exec.Command(socketfilterfw, "--add", exe).Run()
		if out, err := exec.Command(socketfilterfw, "--unblockapp", exe).CombinedOutput(); err != nil {
			fwErr = fmt.Errorf("socketfilterfw: %v %s", err, out)
		}
	})
	return fwErr
}

func firewallChecks(fwManaged bool) []proto.SystemCheck {
	fw := proto.SystemCheck{ID: "firewall", OK: fwErr == nil, Warn: true, Detail: "macOS application firewall"}
	if fwErr != nil {
		fw.Detail = fwErr.Error()
	}
	return []proto.SystemCheck{fw}
}

// Fix repairs a failing system check (id as in SystemChecks).
func (m *Manager) Fix(id string) (string, error) {
	return "", fmt.Errorf("no automatic fix for %q", id)
}
