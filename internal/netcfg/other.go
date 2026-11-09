//go:build !linux && !windows && !darwin

package netcfg

import (
	"errors"
	"net/netip"

	"openv6/internal/proto"
)

const SocketBypass = false

var defaultRunner = func(args ...string) error { return errors.New("unsupported platform") }

func (m *Manager) Detect(configured string) proto.NetState {
	st := proto.NetState{VPNs: detectVPNs(), Bypass: "off"}
	st.Interfaces = interfacesFrom(netAddrs(), m.managedSet())
	st.PrimaryIface = choosePrimary(st.Interfaces, configured, "")
	st.EgressIface = egressByUDP()
	return st
}

func (m *Manager) Apply(d Desired) (map[netip.Addr]error, []error) {
	if len(d.Addrs) == 0 {
		return nil, nil
	}
	return nil, []error{errors.New("this operating system cannot run proxies yet")}
}

func (m *Manager) ApplyBypass(d BypassDesired) error    { return nil }
func TuneKernel() []string                              { return nil }
func UFWActive() bool                                   { return false }
func (m *Manager) SyncFirewall(ports []int) error       { return nil }
func toolCheck(dry bool) proto.SystemCheck              { return proto.SystemCheck{ID: "platform", OK: false} }
func isAdmin() bool                                     { return false }
func firewallChecks(fwManaged bool) []proto.SystemCheck { return nil }

func (m *Manager) Fix(id string) (string, error) { return "", errors.New("no automatic fix for " + id) }
