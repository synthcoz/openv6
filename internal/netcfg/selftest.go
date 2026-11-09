package netcfg

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
)

// SelfTestBypass builds a fake home network and a WARP-like tunnel that
// captures the IPv6 default route, then checks that openv6's policy routing
// keeps proxy traffic on the physical interface while the machine's own
// traffic stays in the VPN. It must run inside a private network namespace
// ("openv6 selftest" re-executes itself under `unshare -n`), so the real
// network is never touched. Returns the human-readable steps.
func SelfTestBypass(dataDir string) ([]string, error) {
	var steps []string
	step := func(f string, a ...any) { steps = append(steps, fmt.Sprintf(f, a...)) }
	sh := func(cmd string) (string, error) {
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%s: %v %s", cmd, err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	if _, err := os.Stat("/proc/sys/net/ipv6"); err != nil {
		return steps, fmt.Errorf("this kernel has no IPv6 support")
	}
	for _, c := range []string{
		"ip link set lo up",
		"ip link add eth0 type veth peer name eth0p && ip link set eth0 up && ip link set eth0p up",
		"ip -6 addr add 2405:4802:1:2::10/64 dev eth0 nodad",
		"ip -6 route add default via fe80::1 dev eth0 metric 1024",
		"ip link add CloudflareWARP type veth peer name warpp && ip link set CloudflareWARP up && ip link set warpp up",
		"ip -6 addr add 2606:4700:110:8000::2/128 dev CloudflareWARP nodad",
		"ip -6 route add default dev CloudflareWARP metric 1",
	} {
		if _, err := sh(c); err != nil {
			return steps, err
		}
	}
	step("simulated network: eth0 with 2405:4802:1:2::/64 + a VPN tunnel holding the default route")
	m := NewManager(dataDir)
	st := m.Detect("")
	if st.PrimaryIface != "eth0" || st.Gateway != "fe80::1" || st.EgressIface != "CloudflareWARP" || len(st.VPNs) == 0 || st.VPNs[0].Kind != "warp" {
		return steps, fmt.Errorf("detection: primary=%q gateway=%q egress=%q", st.PrimaryIface, st.Gateway, st.EgressIface)
	}
	step("detection OK: primary=eth0, router=fe80::1, VPN detected: %s (%s)", st.VPNs[0].Kind, st.VPNs[0].Iface)
	proxyIP := "2405:4802:1:2:aaaa:bbbb:cccc:dddd"
	if errs, gerr := m.Apply(Desired{Iface: "eth0", Addrs: addrSet(proxyIP)}); len(errs) > 0 || len(gerr) > 0 {
		return steps, fmt.Errorf("add address: %v %v", errs, gerr)
	}
	routeFrom := func(src string) string {
		out, _ := sh("ip -6 route get 2001:4860:4860::8888 from " + src)
		return out
	}
	if !strings.Contains(routeFrom(proxyIP), "CloudflareWARP") {
		return steps, fmt.Errorf("precondition failed: %s", routeFrom(proxyIP))
	}
	step("without bypass: proxy traffic would enter the VPN (this is the problem)")
	if err := m.ApplyBypass(BypassDesired{Enabled: true, Iface: "eth0", Gateway: st.Gateway,
		OnLink: []string{"2405:4802:1:2::/64"}, Sources: []string{proxyIP + "/128"}}); err != nil {
		return steps, err
	}
	if r := routeFrom(proxyIP); !strings.Contains(r, "dev eth0") || !strings.Contains(r, "fe80::1") {
		return steps, fmt.Errorf("proxy traffic not bypassing the VPN: %s", r)
	}
	step("with bypass: proxy traffic leaves through eth0 via fe80::1 ✓")
	if r := routeFrom("2405:4802:1:2::10"); !strings.Contains(r, "CloudflareWARP") {
		return steps, fmt.Errorf("machine traffic left the VPN: %s", r)
	}
	step("the machine's own traffic still goes through the VPN ✓")
	if err := m.ApplyBypass(BypassDesired{Enabled: true, Iface: "eth0", Gateway: st.Gateway, Sources: []string{proxyIP + "/128"}}); err != nil {
		return steps, err
	}
	if out, _ := sh("ip -6 rule show"); strings.Count(out, "lookup 6464") != 1 {
		return steps, fmt.Errorf("rules not idempotent: %s", out)
	}
	step("re-applying does not duplicate rules ✓")
	_, _ = sh("ip -6 route del default via fe80::1 dev eth0")
	if st2 := m.Detect(""); st2.Gateway != "fe80::1" {
		return steps, fmt.Errorf("router not remembered after the VPN removed the default route")
	}
	step("router remembered when a VPN removes the physical default route ✓")
	m.Cleanup()
	if out, _ := sh("ip -6 rule show; ip -6 addr show dev eth0"); strings.Contains(out, "6464") || strings.Contains(out, "aaaa") {
		return steps, fmt.Errorf("cleanup left rules or addresses: %s", out)
	}
	step("cleanup removes every rule and address ✓")
	return steps, nil
}

func addrSet(ips ...string) map[netip.Addr]bool {
	m := map[netip.Addr]bool{}
	for _, s := range ips {
		m[netip.MustParseAddr(s)] = true
	}
	return m
}
