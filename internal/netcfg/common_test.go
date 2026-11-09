package netcfg

import (
	"testing"

	"openv6/internal/proto"
)

func TestVPNKindCommon(t *testing.T) {
	for name, want := range map[string]string{"CloudflareWARP": "warp", "Tailscale": "tailscale", "NordLynx": "nordvpn",
		"ProtonVPN": "protonvpn", "OpenVPN Wintun": "openvpn", "utun4": "other", "Ethernet": "", "Wi-Fi": "", "en0": ""} {
		if got := vpnKind(name); got != want {
			t.Errorf("vpnKind(%q)=%q want %q", name, got, want)
		}
	}
	ifs := []proto.Interface{{Name: "Wi-Fi", Prefixes: []string{"2405:4802:1:2::/64"}}, {Name: "Ethernet", Prefixes: []string{"2405:4802:1:3::/64"}}}
	if got := choosePrimary(ifs, "", "Ethernet"); got != "Ethernet" {
		t.Fatalf("primary %q", got)
	}
}
