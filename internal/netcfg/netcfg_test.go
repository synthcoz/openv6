package netcfg

import (
	"bufio"

	"net/netip"
	"openv6/internal/proto"
	"strings"
	"testing"
)

// Sample /proc/net/if_inet6 from a home machine on FPT: SLAAC /64 address,
// a temporary privacy address, link-local, loopback and one address we added.
const sample = `24054802abcd12340211223344556677 02 40 00 00     enp3s0
24054802abcd12348a1b2c3d4e5f6071 02 40 00 01     enp3s0
fe800000000000000211223344556677 02 40 20 80     enp3s0
00000000000000000000000000000001 01 80 10 80       lo
24054802abcd1234deadbeefcafe0001 02 80 00 a2     enp3s0
`

func TestInterfacesFrom(t *testing.T) {
	addrs, err := parseIfInet6(strings.NewReader(sample))
	if err != nil || len(addrs) != 5 {
		t.Fatalf("parse: %v %d", err, len(addrs))
	}
	managed := map[netip.Addr]bool{netip.MustParseAddr("2405:4802:abcd:1234:dead:beef:cafe:1"): true}
	ifs := interfacesFrom(addrs, managed)
	if len(ifs) != 1 || ifs[0].Name != "enp3s0" {
		t.Fatalf("ifaces %+v", ifs)
	}
	if len(ifs[0].Prefixes) != 1 || ifs[0].Prefixes[0] != "2405:4802:abcd:1234::/64" {
		t.Fatalf("prefixes %v", ifs[0].Prefixes)
	}
	if len(ifs[0].Addrs) != 2 { // SLAAC + temporary, not the managed one
		t.Fatalf("addrs %v", ifs[0].Addrs)
	}
}

func TestApplyCommands(t *testing.T) {
	m := NewManager(t.TempDir())
	var cmds []string
	m.DryRun = true
	m.Run = func(a ...string) error { cmds = append(cmds, strings.Join(a, " ")); return nil }
	ip := netip.MustParseAddr("2405:4802:abcd:1234::abcd")
	m.Apply(Desired{Iface: "enp3s0", Addrs: map[netip.Addr]bool{ip: true}, Routes: map[string]bool{"2001:db8::/48": true}})
	m.Apply(Desired{Iface: "enp3s0"})
	want := []string{
		"-6 route replace local 2001:db8::/48 dev lo",
		"-6 addr replace 2405:4802:abcd:1234::abcd/128 dev enp3s0 nodad valid_lft forever preferred_lft 0",
		"-6 route del local 2001:db8::/48 dev lo",
		"-6 addr del 2405:4802:abcd:1234::abcd/128 dev enp3s0",
	}
	if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(cmds, "\n"))
	}
}

func TestVPNKindAndPrimary(t *testing.T) {
	for name, want := range map[string]string{"CloudflareWARP": "warp", "tailscale0": "tailscale", "wg0": "wireguard",
		"nordlynx": "nordvpn", "proton0": "protonvpn", "ipv6leakintrf0": "protonvpn", "tun0": "openvpn", "ztabc": "zerotier",
		"enp3s0": "", "wlp2s0": "", "eth0": ""} {
		if got := vpnKind(name); got != want {
			t.Errorf("vpnKind(%s)=%q want %q", name, got, want)
		}
	}
	ifs := []proto.Interface{
		{Name: "CloudflareWARP", Prefixes: []string{"2606:4700:110:8000::/64"}},
		{Name: "enp3s0", Prefixes: []string{"2405:4802:1:2::/64"}},
	}
	// WARP owns the default route: still pick the physical NIC
	if got := choosePrimary(ifs, "", "CloudflareWARP"); got != "enp3s0" {
		t.Fatalf("primary %q", got)
	}
	if got := choosePrimary(ifs, "wlp2s0", "enp3s0"); got != "wlp2s0" {
		t.Fatalf("configured iface ignored: %q", got)
	}
}

func TestULAExcluded(t *testing.T) {
	// Tailscale ULA must never become a proxy prefix
	addrs, _ := parseIfInet6(strings.NewReader("fd7a115ca1e000000000000000000001 05 40 00 80 tailscale0\n"))
	if ifs := interfacesFrom(addrs, nil); len(ifs) != 0 {
		t.Fatalf("ULA leaked: %+v", ifs)
	}
}

func TestParseDefaultRoutes(t *testing.T) {
	in := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000211223344556677 00000400 00000001 00000000 00000003 enp3s0\n" +
		"00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000001 00000001 00000000 00000001 CloudflareWARP\n"
	rs := parseDefaultRoutes(bufio.NewScanner(strings.NewReader(in)))
	if len(rs) != 2 || rs[0].Iface != "CloudflareWARP" || rs[1].Gateway != "fe80::211:2233:4455:6677" {
		t.Fatalf("%+v", rs)
	}
}

func TestParseRuleSelectors(t *testing.T) {
	out := "100:\tfrom 2405:4802:1:2::abcd lookup 6464\n100:\tfrom 2001:db8::/48 lookup 6464\n"
	m := parseRuleSelectors(out)
	if !m["2405:4802:1:2::abcd/128"] || !m["2001:db8::/48"] || len(m) != 2 {
		t.Fatalf("%v", m)
	}
}
