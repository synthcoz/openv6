//go:build darwin

package netcfg

import "testing"

func TestParseRouteGet(t *testing.T) {
	out := "   route to: default\ndestination: default\n       mask: default\n    gateway: fe80::1%en0\n  interface: en0\n      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING>\n"
	ifc, gw := parseRouteGet(out)
	if ifc != "en0" || gw != "fe80::1" {
		t.Fatalf("%q %q", ifc, gw)
	}
}
