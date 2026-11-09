//go:build windows

package netcfg

import "testing"

func TestParseNetshRoutes(t *testing.T) {
	out := `
Publish  Type      Met  Prefix                    Idx  Gateway/Interface Name
-------  --------  ---  ------------------------  ---  ------------------------
No       Manual    256  ::/0                        1  fe80::1
No       System    256  ::1/128                     1  Loopback Pseudo-Interface 1
`
	rs := parseNetshRoutes(out)
	// interface index 1 is the loopback pseudo-interface on every Windows
	if len(rs) != 1 || rs[0].Gateway != "fe80::1" || rs[0].Metric != 256 {
		t.Fatalf("%+v", rs)
	}
}
