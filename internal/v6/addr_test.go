package v6

import (
	"net/netip"
	"testing"
)

func TestCompose(t *testing.T) {
	cases := []struct {
		pfx    string
		subnet uint64
		suffix uint64
		want   string
	}{
		{"2405:4802:1234:5678::/64", 0, 0x1122334455667788, "2405:4802:1234:5678:1122:3344:5566:7788"},
		{"2405:4802:1234:5600::/56", 0x7f, 1 << 20, "2405:4802:1234:567f::10:0"},
		{"2a01:4f8:aa::/48", 0xbeef, 0xabc12345, "2a01:4f8:aa:beef::abc1:2345"},
	}
	for _, c := range cases {
		got, err := Compose(netip.MustParsePrefix(c.pfx), c.subnet, c.suffix)
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != c.want {
			t.Errorf("%s: got %s want %s", c.pfx, got, c.want)
		}
	}
	if _, err := Compose(netip.MustParsePrefix("2405::/56"), 256, 1); err == nil {
		t.Error("expected out of range error")
	}
	if SubnetCount(netip.MustParsePrefix("2405::/56")) != 256 {
		t.Error("subnet count")
	}
}
