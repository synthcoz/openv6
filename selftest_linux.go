//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"openv6/internal/netcfg"
)

func runSelfTest() error {
	if os.Getenv("OPENV6_SELFTEST_INNER") == "1" {
		dir, _ := os.MkdirTemp("", "openv6-selftest")
		defer os.RemoveAll(dir)
		steps, err := netcfg.SelfTestBypass(dir)
		for _, s := range steps {
			fmt.Println("   ✓", s)
		}
		return err
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("run with sudo: sudo openv6 selftest")
	}
	fmt.Println("== 1) Mạng hiện tại / current network ==")
	m := netcfg.NewManager(filepath.Join(os.TempDir(), "openv6-selftest-ro"))
	st := m.Detect("")
	show := func(k, v string) {
		if v == "" {
			v = "—"
		}
		fmt.Printf("   %-26s %s\n", k, v)
	}
	show("Card mạng chính / primary:", st.PrimaryIface)
	for _, it := range st.Interfaces {
		if it.Name == st.PrimaryIface {
			show("Prefix IPv6:", strings.Join(it.Prefixes, ", "))
		}
	}
	show("Router IPv6:", st.Gateway)
	show("Đường ra hiện tại / egress:", st.EgressIface)
	show("IPv6 bị tắt / disabled:", fmt.Sprint(st.IPv6Disabled))
	var vpns []string
	for _, v := range st.VPNs {
		state := "down"
		if v.Up {
			state = "up"
		}
		vpns = append(vpns, fmt.Sprintf("%s (%s, %s)", v.Kind, v.Iface, state))
	}
	show("VPN:", strings.Join(vpns, "; "))
	if out, err := exec.Command("ip", "-6", "rule", "show", "table", netcfg.BypassTable).Output(); err == nil {
		show("Luật vượt VPN / bypass rules:", fmt.Sprint(strings.Count(string(out), "lookup")))
	}
	fmt.Println()
	fmt.Println("== 2) Thử định tuyến vượt VPN (mạng giả lập) / bypass routing test (isolated) ==")
	if _, err := exec.LookPath("unshare"); err != nil {
		return fmt.Errorf("unshare not found (package util-linux)")
	}
	self, _ := os.Executable()
	cmd := exec.Command("unshare", "-n", self, "selftest")
	cmd.Env = append(os.Environ(), "OPENV6_SELFTEST_INNER=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println("   ✗ KHÔNG ĐẠT / FAILED")
		return fmt.Errorf("bypass self-test failed: %v", err)
	}
	fmt.Println("   => ĐẠT / PASSED: proxy vẫn đi đúng card mạng khi bật VPN (WARP, Tailscale exit node, WireGuard…)")
	return nil
}
