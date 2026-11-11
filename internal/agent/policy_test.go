package agent

import (
	"testing"

	"openv6/internal/proto"
)

// No IPv4 setting may fall back to the machine's own IPv4 by accident.
func TestIPv4Policy(t *testing.T) {
	sites := []string{"check.example.com"}
	cases := []struct {
		mode, nat64 string
		off, viaNAT bool
	}{
		{"off", "2001:db8:64::/96", true, false},
		{"nat64", "2001:db8:64::/96", false, true},
		{"nat64", "", true, false},                 // no prefix: off, not a leak
		{"nat64", "2001:db8:64::/64", true, false}, // not a /96: off
		{"direct", "2001:db8:64::/96", false, false},
		{"", "", false, false}, // older panel: machine IPv4 as before
	}
	for _, c := range cases {
		list, p, off := ipv4Policy(&proto.SyncResponse{IPv4Mode: c.mode, NAT64: c.nat64, IPv4Sites: sites})
		if off != c.off || p.IsValid() != c.viaNAT {
			t.Errorf("mode %q nat64 %q: off=%v nat64=%v", c.mode, c.nat64, off, p)
		}
		if off && list != nil {
			t.Errorf("mode %q: IPv4 sites must be dropped when IPv4 is off", c.mode)
		}
		if !off && !list.Host("check.example.com") {
			t.Errorf("mode %q: IPv4 sites lost", c.mode)
		}
	}
}
