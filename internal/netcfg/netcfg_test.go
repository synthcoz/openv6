package netcfg

import (
	"net/netip"
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
