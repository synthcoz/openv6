//go:build windows || darwin

package netcfg

import (
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestRealAddressLifecycle adds a ULA proxy address to a real interface,
// binds a socket to it, and removes it. Needs Administrator/root, so it only
// runs in CI or when OPENV6_SYSTEM_TEST=1.
func TestRealAddressLifecycle(t *testing.T) {
	if os.Getenv("OPENV6_SYSTEM_TEST") != "1" {
		t.Skip("set OPENV6_SYSTEM_TEST=1 (needs admin)")
	}
	iface := pickInterface(t)
	t.Logf("using interface %q", iface)
	m := NewManager(t.TempDir())
	st := m.Detect("")
	t.Logf("detect: primary=%q default=%q egress=%q gw=%q vpns=%+v", st.PrimaryIface, st.DefaultIface, st.EgressIface, st.Gateway, st.VPNs)

	ip := netip.MustParseAddr("fd00:6f76:36::1234")
	errs, gerr := m.Apply(Desired{Iface: iface, Addrs: map[netip.Addr]bool{ip: true}})
	if len(errs) > 0 || len(gerr) > 0 {
		t.Fatalf("apply: %v %v", errs, gerr)
	}
	found := false
	for _, a := range netAddrs() {
		if a.Addr == ip && a.Iface == iface {
			found = true
		}
	}
	if !found {
		out, _ := exec.Command(listCmd()[0], listCmd()[1:]...).CombinedOutput()
		t.Fatalf("address not present on %s:\n%s", iface, out)
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			"(Get-NetIPAddress -IPAddress "+ip.String()+").SkipAsSource").Output()
		if err != nil || !strings.Contains(string(out), "True") {
			t.Fatalf("SkipAsSource not set: %v %s", err, out)
		}
	}
	// the proxy must be able to use the address as its source
	ln, err := net.Listen("tcp", "["+ip.String()+"]:0")
	if err != nil {
		t.Fatalf("bind to proxy address: %v", err)
	}
	ln.Close()
	// re-applying is idempotent
	if errs, gerr := m.Apply(Desired{Iface: iface, Addrs: map[netip.Addr]bool{ip: true}}); len(errs)+len(gerr) > 0 {
		t.Fatalf("re-apply: %v %v", errs, gerr)
	}
	m.Cleanup()
	for _, a := range netAddrs() {
		if a.Addr == ip {
			t.Fatal("address left behind after cleanup")
		}
	}
	checks := m.SystemChecks(st, nil, true)
	for _, c := range checks {
		t.Logf("check %-18s ok=%v warn=%v %s", c.ID, c.OK, c.Warn, c.Detail)
	}
}

func pickInterface(t *testing.T) string {
	if v := os.Getenv("OPENV6_TEST_IFACE"); v != "" {
		return v
	}
	ifs, _ := net.Interfaces()
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagLoopback != 0 || vpnKind(it.Name) != "" {
			continue
		}
		addrs, _ := it.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() == nil {
				return it.Name // has IPv6 (at least link-local)
			}
		}
	}
	t.Skip("no interface with IPv6")
	return ""
}

func listCmd() []string {
	if runtime.GOOS == "windows" {
		return []string{"netsh", "interface", "ipv6", "show", "addresses"}
	}
	return []string{"ifconfig", "-a"}
}
