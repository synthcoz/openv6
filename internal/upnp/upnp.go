// Package upnp opens proxy ports on the home router (UPnP IGD) so proxies
// with scope "any" are reachable from the Internet over IPv4.
package upnp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"

	"openv6/internal/proto"
)

type igd interface {
	AddPortMappingCtx(ctx context.Context, remoteHost string, extPort uint16, protocol string, intPort uint16, intClient string, enabled bool, desc string, lease uint32) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, extPort uint16, protocol string) error
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	LocalAddr() net.IP
}

const lease = 3600

// Mapper keeps the wanted ports mapped on the router.
type Mapper struct {
	mu      sync.Mutex
	enabled bool
	want    map[int]bool
	mapped  map[int]bool
	state   proto.UPnPState
	kick    chan struct{}
	client  igd
	started bool
}

func New() *Mapper {
	return &Mapper{want: map[int]bool{}, mapped: map[int]bool{}, kick: make(chan struct{}, 1)}
}

// Apply sets the wanted state; work happens in the background.
func (m *Mapper) Apply(enabled bool, ports []int) {
	m.mu.Lock()
	want := map[int]bool{}
	if enabled {
		for _, p := range ports {
			want[p] = true
		}
	}
	changed := enabled != m.enabled || len(want) != len(m.want)
	for p := range want {
		if !m.want[p] {
			changed = true
		}
	}
	m.enabled, m.want = enabled, want
	if !m.started && enabled {
		m.started = true
		go m.loop()
	}
	m.mu.Unlock()
	if changed {
		select {
		case m.kick <- struct{}{}:
		default:
		}
	}
}

// Status is nil when UPnP is off.
func (m *Mapper) Status() *proto.UPnPState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled {
		return nil
	}
	st := m.state
	if st.State == "" {
		st.State = "pending"
	}
	return &st
}

func (m *Mapper) loop() {
	t := time.NewTicker(20 * time.Minute)
	defer t.Stop()
	for {
		m.sync()
		select {
		case <-m.kick:
		case <-t.C:
		}
	}
}

func discover(ctx context.Context) (igd, error) {
	if cs, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return cs[0], nil
	}
	if cs, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return cs[0], nil
	}
	if cs, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return cs[0], nil
	}
	return nil, errNone
}

var errNone = errors.New("no UPnP router found (turn on UPnP in the router settings)")

func (m *Mapper) sync() {
	m.mu.Lock()
	enabled := m.enabled
	want := map[int]bool{}
	for p := range m.want {
		want[p] = true
	}
	mapped := map[int]bool{}
	for p := range m.mapped {
		mapped[p] = true
	}
	cl := m.client
	m.mu.Unlock()

	if !enabled && len(mapped) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if cl == nil {
		var err error
		if cl, err = discover(ctx); err != nil {
			m.setState(proto.UPnPState{State: "none", Error: err.Error()})
			return
		}
	}
	st := proto.UPnPState{State: "ok"}
	if ip, err := cl.GetExternalIPAddressCtx(ctx); err == nil {
		st.ExternalIP = ip
		if a := net.ParseIP(ip); a != nil && (a.IsPrivate() || cgnat.Contains(a) || a.IsUnspecified()) {
			st.State = "cgnat"
			st.Error = fmt.Sprintf("router WAN address %s is private: the ISP uses CGNAT, ports cannot be opened from home (use a relay VPS or ask the ISP for a public IPv4)", ip)
		}
	}
	local := ""
	if a := cl.LocalAddr(); a != nil {
		local = a.String()
	}
	var errs []string
	for p := range mapped {
		if !want[p] {
			if err := cl.DeletePortMappingCtx(ctx, "", uint16(p), "TCP"); err != nil {
				log.Printf("upnp: delete %d: %v", p, err)
			}
			delete(mapped, p)
		}
	}
	ports := make([]int, 0, len(want))
	for p := range want {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	for _, p := range ports {
		err := cl.AddPortMappingCtx(ctx, "", uint16(p), "TCP", uint16(p), local, true, "openv6", lease)
		if err != nil { // some routers only accept permanent leases
			err = cl.AddPortMappingCtx(ctx, "", uint16(p), "TCP", uint16(p), local, true, "openv6", 0)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%d: %v", p, err))
			continue
		}
		mapped[p] = true
	}
	if len(errs) > 0 {
		if st.State == "ok" {
			st.State = "error"
		}
		if st.Error == "" {
			st.Error = "router refused port " + errs[0]
		}
		if len(mapped) == 0 && len(want) > 0 {
			cl = nil // rediscover next time
		}
	}
	st.Mapped = len(mapped)
	m.mu.Lock()
	m.mapped, m.client = mapped, cl
	m.state = st
	m.mu.Unlock()
}

func (m *Mapper) setState(st proto.UPnPState) {
	m.mu.Lock()
	m.state = st
	m.mu.Unlock()
}

var cgnat = func() *net.IPNet { _, n, _ := net.ParseCIDR("100.64.0.0/10"); return n }()
