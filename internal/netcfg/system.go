package netcfg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"openv6/internal/proto"
)

// sysctlMin lists kernel settings raised (never lowered) for many addresses
// and connections. gc_thresh controls the IPv6 neighbor table: with the default
// (1024 hard limit on some distros) busy proxies start dropping neighbors.
var sysctlMin = map[string]int{
	"net/ipv6/neigh/default/gc_thresh1": 1024,
	"net/ipv6/neigh/default/gc_thresh2": 4096,
	"net/ipv6/neigh/default/gc_thresh3": 8192,
	"net/ipv6/route/max_size":           65536,
	"net/core/somaxconn":                4096,
	"net/ipv4/tcp_max_syn_backlog":      4096,
	"fs/file-max":                       1048576,
}

// TuneKernel raises kernel limits. Returns the settings it could not apply.
func TuneKernel() []string {
	var failed []string
	keys := make([]string, 0, len(sysctlMin))
	for k := range sysctlMin {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := filepath.Join("/proc/sys", k)
		b, err := os.ReadFile(p)
		if err != nil {
			continue // not present on this kernel
		}
		cur, _ := strconv.Atoi(strings.Fields(string(b) + " 0")[0])
		if cur >= sysctlMin[k] {
			continue
		}
		if err := os.WriteFile(p, []byte(strconv.Itoa(sysctlMin[k])), 0o644); err != nil {
			failed = append(failed, strings.ReplaceAll(k, "/", "."))
		}
	}
	return failed
}

// SysctlConf is written to /etc/sysctl.d by "openv6 install" so the values
// survive reboots even before the service starts.
func SysctlConf() string {
	var sb strings.Builder
	sb.WriteString("# written by openv6 install\n")
	keys := make([]string, 0, len(sysctlMin))
	for k := range sysctlMin {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s = %d\n", strings.ReplaceAll(k, "/", "."), sysctlMin[k])
	}
	return sb.String()
}

// ---- firewall (ufw) ----

const ufwComment = "openv6"

// UFWActive reports whether ufw is installed and enabled.
func UFWActive() bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false
	}
	out, err := exec.Command("ufw", "status").CombinedOutput()
	return err == nil && strings.Contains(string(out), "Status: active")
}

// SyncFirewall makes sure exactly the given TCP ports are opened by openv6
// rules (identified by their comment). Other ufw rules are never touched.
func (m *Manager) SyncFirewall(ports []int) error {
	if m.DryRun || !UFWActive() {
		return nil
	}
	out, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ufw status: %v", err)
	}
	have := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "# "+ufwComment) || strings.Contains(line, "(v6)") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSuffix(f[0], "/tcp")); err == nil {
			have[p] = true
		}
	}
	want := map[int]bool{}
	for _, p := range ports {
		want[p] = true
	}
	var errs []string
	for p := range want {
		if !have[p] {
			if b, err := exec.Command("ufw", "allow", fmt.Sprintf("%d/tcp", p), "comment", ufwComment).CombinedOutput(); err != nil {
				errs = append(errs, strings.TrimSpace(string(b)))
			}
		}
	}
	for p := range have {
		if !want[p] {
			_ = exec.Command("ufw", "--force", "delete", "allow", fmt.Sprintf("%d/tcp", p)).Run()
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("ufw: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ---- self-diagnosis ----

// SystemChecks returns the machine diagnosis displayed on the node page.
func (m *Manager) SystemChecks(st proto.NetState, iface string, tuneFailed []string, fwManaged bool) []proto.SystemCheck {
	var c []proto.SystemCheck
	_, ipErr := exec.LookPath("ip")
	c = append(c, proto.SystemCheck{ID: "iproute2", OK: ipErr == nil || m.DryRun})
	c = append(c, proto.SystemCheck{ID: "ipv6_route", OK: st.HasV6Route, Detail: st.DefaultIface})
	pfx := ""
	for _, it := range st.Interfaces {
		if it.Name == iface && len(it.Prefixes) > 0 {
			pfx = it.Prefixes[0]
		}
	}
	c = append(c, proto.SystemCheck{ID: "ipv6_prefix", OK: pfx != "", Detail: pfx})
	c = append(c, proto.SystemCheck{ID: "root", OK: os.Geteuid() == 0 || m.DryRun})
	c = append(c, proto.SystemCheck{ID: "kernel_tuning", OK: len(tuneFailed) == 0, Warn: true, Detail: strings.Join(tuneFailed, ", ")})
	fw := proto.SystemCheck{ID: "firewall", OK: true, Warn: true, Detail: "inactive"}
	if UFWActive() {
		fw.Detail = "ufw active"
		if fwManaged {
			fw.Detail = "ufw active, ports managed by openv6"
		} else {
			fw.OK = false
		}
	}
	c = append(c, fw)
	return c
}
