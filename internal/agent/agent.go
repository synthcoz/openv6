// Package agent runs on every node: it polls the panel, installs IPv6
// addresses, runs the proxy listeners and reports stats.
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"openv6/internal/netcfg"
	"openv6/internal/proto"
	"openv6/internal/proxy"
)

type Config struct {
	PanelURL string
	Token    string
	DataDir  string
	DryRun   bool // don't touch kernel networking (development on non-Linux)
	// InsecureTLS skips certificate checks (self-signed panel certificate).
	InsecureTLS bool
}

type Agent struct {
	cfg     Config
	client  *http.Client
	net     *netcfg.Manager
	proxies *proxy.Manager

	mu       sync.Mutex
	lastSent map[int64]proto.ProxyStats
	applied  map[int64]proto.Applied
	results  []proto.CommandResult
	errors   []string
	running  map[string]bool // command ids in progress
	specs    map[int64]proto.ProxySpec

	pubV4   string
	pubV4At time.Time
}

func New(cfg Config) *Agent {
	_ = os.MkdirAll(cfg.DataDir, 0o700)
	n := netcfg.NewManager(cfg.DataDir)
	if cfg.DryRun {
		n.DryRun = true
		n.Run = func(args ...string) error { log.Printf("[dry-run] ip %s", strings.Join(args, " ")); return nil }
	}
	return &Agent{
		cfg:      cfg,
		client:   &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}}},
		net:      n,
		proxies:  proxy.NewManager(),
		lastSent: map[int64]proto.ProxyStats{},
		applied:  map[int64]proto.Applied{},
		running:  map[string]bool{},
		specs:    map[int64]proto.ProxySpec{},
	}
}

// Run polls the panel until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	log.Printf("agent: panel %s", a.cfg.PanelURL)
	interval := 5 * time.Second
	failures := 0
	for {
		resp, err := a.sync(ctx)
		if err != nil {
			failures++
			if failures == 1 || failures%12 == 0 {
				log.Printf("agent: sync failed (%d): %v", failures, err)
			}
			// keep serving last known config while the panel is unreachable
		} else {
			if failures > 0 {
				log.Printf("agent: panel reachable again")
			}
			failures = 0
			a.apply(resp)
			if resp.Interval > 0 {
				interval = time.Duration(resp.Interval) * time.Second
			}
		}
		select {
		case <-ctx.Done():
			a.proxies.Close()
			return
		case <-time.After(interval):
		}
	}
}

func (a *Agent) sync(ctx context.Context) (*proto.SyncResponse, error) {
	host, _ := os.Hostname()
	st := a.net.Detect()
	st.LANv4 = lanIPv4()
	st.PublicV4 = a.publicV4()

	counters := a.proxies.Counters()
	a.mu.Lock()
	stats := map[int64]proto.ProxyStats{}
	current := map[int64]proto.ProxyStats{}
	for id, c := range counters {
		cur := proto.ProxyStats{Up: c.Up.Load(), Down: c.Down.Load(), Conns: c.Conns.Load(), Active: c.Active.Load()}
		current[id] = cur
		last := a.lastSent[id]
		if cur.Up < last.Up || cur.Down < last.Down || cur.Conns < last.Conns { // listener recreated
			last = proto.ProxyStats{}
		}
		stats[id] = proto.ProxyStats{Up: cur.Up - last.Up, Down: cur.Down - last.Down, Conns: cur.Conns - last.Conns, Active: cur.Active}
	}
	applied := map[int64]proto.Applied{}
	for k, v := range a.applied {
		applied[k] = v
	}
	results := a.results
	errs := a.errors
	a.mu.Unlock()

	req := proto.SyncRequest{
		AgentVersion: proto.Version, Hostname: host, OS: runtime.GOOS + "/" + runtime.GOARCH,
		Net: st, Stats: stats, Applied: applied, Results: results, Errors: errs,
	}
	body, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(a.cfg.PanelURL, "/")+"/api/agent/sync", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	hr.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(hr)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("panel returned %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	var out proto.SyncResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	// success: commit stats baseline and drop delivered results
	a.mu.Lock()
	a.lastSent = current
	a.results = a.results[len(results):]
	a.mu.Unlock()
	return &out, nil
}

func (a *Agent) apply(r *proto.SyncResponse) {
	st := a.net.Detect()
	iface := r.Iface
	if iface == "" {
		iface = st.DefaultIface
	}
	pools := map[int64]proto.PoolSpec{}
	for _, p := range r.Pools {
		pools[p.ID] = p
	}
	want := netcfg.Desired{Iface: iface, Addrs: map[netip.Addr]bool{}, Routes: map[string]bool{}}
	for _, p := range r.Pools {
		if p.Mode == "anyip" && p.CIDR != "" {
			want.Routes[p.CIDR] = true
		}
	}
	applied := map[int64]proto.Applied{}
	var cfgs []proxy.Config
	specs := map[int64]proto.ProxySpec{}
	for _, s := range r.Proxies {
		specs[s.ID] = s
		ip, err := netip.ParseAddr(s.IP)
		if err != nil {
			applied[s.ID] = proto.Applied{IP: s.IP, Error: "no IPv6 prefix available yet"}
			continue
		}
		if p, ok := pools[s.PoolID]; !ok || p.Mode != "anyip" {
			want.Addrs[ip] = true
		}
		cfgs = append(cfgs, proxy.Config{ID: s.ID, Port: s.Port, Protocol: s.Protocol, User: s.User, Pass: s.Pass, OutIP: ip, IPv4Fallback: s.IPv4Fallback})
	}
	addrErrs, errs := a.net.Apply(want)
	var ok []proxy.Config
	for _, c := range cfgs {
		if err := addrErrs[c.OutIP]; err != nil {
			applied[c.ID] = proto.Applied{IP: c.OutIP.String(), Error: err.Error()}
			continue
		}
		ok = append(ok, c)
	}
	var allow []netip.Prefix
	for _, s := range r.AllowCIDRs {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
			allow = append(allow, p.Masked())
		} else if ad, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
			allow = append(allow, netip.PrefixFrom(ad, ad.BitLen()))
		}
	}
	a.proxies.SetAllow(allow)
	listenErrs := a.proxies.Apply(ok)
	for _, c := range ok {
		if err := listenErrs[c.ID]; err != nil {
			applied[c.ID] = proto.Applied{IP: c.OutIP.String(), Error: "listen: " + err.Error()}
		} else {
			applied[c.ID] = proto.Applied{IP: c.OutIP.String(), OK: true}
		}
	}
	var errStrs []string
	for _, e := range errs {
		errStrs = append(errStrs, e.Error())
	}
	a.mu.Lock()
	a.applied = applied
	a.errors = errStrs
	a.specs = specs
	for _, c := range r.Commands {
		if a.running[c.ID] {
			continue
		}
		a.running[c.ID] = true
		go a.runCommand(c)
	}
	a.mu.Unlock()
}

func (a *Agent) runCommand(c proto.Command) {
	var res proto.CommandResult
	res.ID = c.ID
	switch c.Type {
	case "test_proxy":
		a.mu.Lock()
		spec, ok := a.specs[c.ProxyID]
		a.mu.Unlock()
		if !ok {
			res.Output = "proxy not running on this node"
			break
		}
		ip, err := netip.ParseAddr(spec.IP)
		if err != nil {
			res.Output = "proxy has no IPv6 address yet"
			break
		}
		seen, err := CheckIP(ip, 10*time.Second)
		if err != nil {
			res.Output = err.Error()
		} else {
			res.OK = seen == ip.String()
			res.Output = seen
			if !res.OK {
				res.Output = fmt.Sprintf("expected %s but target saw %s", ip, seen)
			}
		}
	case "test_v6":
		seen, err := CheckIP(netip.Addr{}, 10*time.Second)
		if err != nil {
			res.Output = err.Error()
		} else {
			res.OK, res.Output = true, seen
		}
	default:
		res.Output = "unknown command"
	}
	a.mu.Lock()
	a.results = append(a.results, res)
	delete(a.running, c.ID)
	a.mu.Unlock()
}

// CheckIP asks an IPv6-only echo service which address it sees. If src is
// valid the connection is bound to it.
func CheckIP(src netip.Addr, timeout time.Duration) (string, error) {
	d := &net.Dialer{Timeout: timeout}
	if src.IsValid() {
		d.LocalAddr = &net.TCPAddr{IP: src.AsSlice()}
	}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp6", addr)
		},
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: timeout}
	var lastErr error
	for _, u := range []string{"https://api6.ipify.org", "https://ipv6.icanhazip.com"} {
		res, err := cl.Get(u)
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 128))
		res.Body.Close()
		if res.StatusCode == 200 {
			if ip, err := netip.ParseAddr(strings.TrimSpace(string(b))); err == nil {
				return ip.String(), nil
			}
		}
		lastErr = fmt.Errorf("%s: %s", u, res.Status)
	}
	return "", lastErr
}

func (a *Agent) publicV4() string {
	if time.Since(a.pubV4At) < 10*time.Minute {
		return a.pubV4
	}
	a.pubV4At = time.Now()
	tr := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp4", addr)
	}}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	res, err := cl.Get("https://api4.ipify.org")
	if err != nil {
		return a.pubV4
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64))
	if ip, err := netip.ParseAddr(strings.TrimSpace(string(b))); err == nil && ip.Is4() {
		a.pubV4 = ip.String()
	}
	return a.pubV4
}

func lanIPv4() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := it.Addrs()
		for _, ad := range addrs {
			if ipn, ok := ad.(*net.IPNet); ok {
				if v4 := ipn.IP.To4(); v4 != nil {
					out = append(out, v4.String())
				}
			}
		}
	}
	return out
}
