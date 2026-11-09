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

	"openv6/internal/logbuf"
	"openv6/internal/netcfg"
	"openv6/internal/proto"
	"openv6/internal/proxy"
	"openv6/internal/update"
)

type Config struct {
	PanelURL string
	Token    string
	DataDir  string
	DryRun   bool // don't touch kernel networking (development on non-Linux)
	// InsecureTLS skips certificate checks (self-signed panel certificate).
	InsecureTLS bool
	// Logs, when set, is shipped to the panel (remote agents) so it can be
	// viewed in the web UI. The local agent shares the panel's buffer instead.
	Logs *logbuf.Buffer
	// AllowUpdate lets the panel replace this binary (remote agents only).
	AllowUpdate bool
	// Direct, when set, syncs in-process (the agent embedded in the panel)
	// instead of over HTTP.
	Direct func(ctx context.Context, req *proto.SyncRequest) (*proto.SyncResponse, error)
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

	logSeq         uint64
	tuneFailed     []string
	fwManaged      bool
	iface          string
	restartPending bool
	reapply        bool
	bypassStatus   string
	bypassErr      string
	startedAt      time.Time
	bindIf         int
	testSem        chan struct{}
}

func New(cfg Config) *Agent {
	_ = os.MkdirAll(cfg.DataDir, 0o700)
	n := netcfg.NewManager(cfg.DataDir)
	if cfg.DryRun {
		n.DryRun = true
		n.Run = func(args ...string) error { log.Printf("[dry-run] ip %s", strings.Join(args, " ")); return nil }
	}
	return &Agent{
		cfg:       cfg,
		client:    &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}}},
		net:       n,
		proxies:   proxy.NewManager(),
		lastSent:  map[int64]proto.ProxyStats{},
		applied:   map[int64]proto.Applied{},
		running:   map[string]bool{},
		specs:     map[int64]proto.ProxySpec{},
		testSem:   make(chan struct{}, 8),
		startedAt: time.Now(),
	}
}

// Run polls the panel until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	log.Printf("agent %s: panel %s", proto.Version, a.cfg.PanelURL)
	if !a.cfg.DryRun {
		a.tuneFailed = netcfg.TuneKernel()
		if len(a.tuneFailed) > 0 {
			log.Printf("agent: could not raise kernel limits: %s", strings.Join(a.tuneFailed, ", "))
		}
	}
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
			a.mu.Lock()
			restart := a.restartPending && len(a.results) == 0
			a.mu.Unlock()
			if restart {
				log.Printf("agent: restarting into the updated binary")
				a.proxies.Close()
				os.Exit(0) // systemd (Restart=always) starts the new binary
			}
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
	a.mu.Lock()
	cfgIface := a.iface
	a.mu.Unlock()
	st := a.net.Detect(cfgIface)
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
	if a.bypassStatus != "" {
		st.Bypass, st.BypassError = a.bypassStatus, a.bypassErr
	}
	checks := a.net.SystemChecks(st, a.tuneFailed, a.fwManaged)
	var logs []string
	nextSeq := a.logSeq
	if a.cfg.Logs != nil {
		logs, nextSeq = a.cfg.Logs.Since(a.logSeq, 300)
	}
	a.mu.Unlock()

	req := proto.SyncRequest{
		AgentVersion: proto.Version, Hostname: host, OS: runtime.GOOS + "/" + runtime.GOARCH, Arch: runtime.GOARCH,
		Net: st, Stats: stats, Applied: applied, Results: results, Errors: errs, Checks: checks, Logs: logs,
		StartedAt: a.startedAt.Unix(),
	}
	var out proto.SyncResponse
	if a.cfg.Direct != nil {
		resp, err := a.cfg.Direct(ctx, &req)
		if err != nil {
			return nil, err
		}
		out = *resp
	} else if err := a.post(ctx, &req, &out); err != nil {
		return nil, err
	}
	// success: commit stats baseline and drop delivered results
	a.mu.Lock()
	a.lastSent = current
	a.results = a.results[len(results):]
	a.logSeq = nextSeq
	a.mu.Unlock()
	return &out, nil
}

func (a *Agent) post(ctx context.Context, req *proto.SyncRequest, out *proto.SyncResponse) error {
	body, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(a.cfg.PanelURL, "/")+"/api/agent/sync", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	hr.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(hr)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("panel returned %s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (a *Agent) apply(r *proto.SyncResponse) {
	a.mu.Lock()
	a.iface = r.Iface
	reapply := a.reapply
	a.reapply = false
	a.mu.Unlock()
	if reapply {
		log.Printf("agent: re-applying network configuration from scratch")
		a.net.Cleanup()
	}
	st := a.net.Detect(r.Iface)
	iface := st.PrimaryIface
	pools := map[int64]proto.PoolSpec{}
	for _, p := range r.Pools {
		pools[p.ID] = p
	}
	want := netcfg.Desired{Iface: iface, Addrs: map[netip.Addr]bool{}, Routes: map[string]bool{}}
	bypass := netcfg.BypassDesired{Enabled: r.Bypass, Iface: iface, Gateway: st.Gateway}
	for _, p := range r.Pools {
		if p.Mode == "anyip" && p.CIDR != "" {
			want.Routes[p.CIDR] = true
			bypass.Sources = append(bypass.Sources, p.CIDR)
		}
	}
	for _, it := range st.Interfaces {
		if it.Name == iface {
			bypass.OnLink = append(bypass.OnLink, it.Prefixes...)
		}
	}
	// Windows/macOS: VPN bypass by pinning sockets to the physical interface
	bindIf := 0
	if netcfg.SocketBypass && r.Bypass {
		bindIf = netcfg.IfIndex(iface)
	}
	applied := map[int64]proto.Applied{}
	var cfgs []proxy.Config
	specs := map[int64]proto.ProxySpec{}
	for _, s := range r.Proxies {
		specs[s.ID] = s
		ip, err := netip.ParseAddr(s.IP)
		if err != nil {
			applied[s.ID] = proto.Applied{IP: s.IP, Error: "no IPv6 prefix available yet", Code: "no_prefix"}
			continue
		}
		if p, ok := pools[s.PoolID]; !ok || p.Mode != "anyip" {
			want.Addrs[ip] = true
			bypass.Sources = append(bypass.Sources, ip.String()+"/128")
		}
		c := proxy.Config{ID: s.ID, Port: s.Port, Protocol: s.Protocol, User: s.User, Pass: s.Pass, OutIP: ip,
			IPv4Fallback: s.IPv4Fallback, Scope: s.Scope, BindIf: bindIf}
		if len(r.DNS) > 0 {
			c.Resolver = proxy.NewResolver(r.DNS, ip, bindIf)
		}
		cfgs = append(cfgs, c)
	}
	addrErrs, errs := a.net.Apply(want)
	if st.IPv6Disabled {
		errs = append(errs, fmt.Errorf("IPv6 is disabled on this machine (net.ipv6.conf.*.disable_ipv6=1)"))
	}
	var ok []proxy.Config
	for _, c := range cfgs {
		if err := addrErrs[c.OutIP]; err != nil {
			applied[c.ID] = proto.Applied{IP: c.OutIP.String(), Error: err.Error(), Code: "addr_add_failed"}
			continue
		}
		ok = append(ok, c)
	}
	// keep proxy traffic on the physical NIC even if a VPN grabs the default route
	bypassStatus, bypassErr := "off", ""
	if netcfg.SocketBypass && r.Bypass {
		if bindIf > 0 {
			bypassStatus = "ok"
		} else {
			bypassStatus, bypassErr = "error", "interface "+iface+" not found"
		}
	} else if !a.cfg.DryRun && !netcfg.SocketBypass {
		if r.Bypass {
			if err := a.net.ApplyBypass(bypass); err != nil {
				bypassStatus, bypassErr = "error", err.Error()
				log.Printf("agent: VPN bypass routing: %v", err)
			} else {
				bypassStatus = "ok"
			}
		} else {
			_ = a.net.ApplyBypass(netcfg.BypassDesired{})
		}
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
	if r.Firewall {
		var ports []int
		for _, c := range ok {
			if c.Scope != "local" {
				ports = append(ports, c.Port)
			}
		}
		if err := a.net.SyncFirewall(ports); err != nil {
			errs = append(errs, err)
		}
	}
	for _, c := range ok {
		if err := listenErrs[c.ID]; err != nil {
			applied[c.ID] = proto.Applied{IP: c.OutIP.String(), Error: "listen: " + err.Error(), Code: proxy.Classify(err)}
		} else {
			applied[c.ID] = proto.Applied{IP: c.OutIP.String(), OK: true}
		}
	}
	var errStrs []string
	for _, e := range errs {
		errStrs = append(errStrs, e.Error())
	}
	a.mu.Lock()
	a.fwManaged = r.Firewall
	a.bypassStatus, a.bypassErr = bypassStatus, bypassErr
	a.applied = applied
	a.errors = errStrs
	a.specs = specs
	a.bindIf = bindIf
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
	res := proto.CommandResult{ID: c.ID}
	switch c.Type {
	case "test_proxy":
		a.testSem <- struct{}{}
		a.mu.Lock()
		spec, ok := a.specs[c.ProxyID]
		a.mu.Unlock()
		if !ok {
			res.Output = "proxy not running on this node"
		} else if ip, err := netip.ParseAddr(spec.IP); err != nil {
			res.Output = "proxy has no IPv6 address yet"
		} else {
			a.mu.Lock()
			bindIf := a.bindIf
			a.mu.Unlock()
			info, err := CheckIP(ip, bindIf, 12*time.Second)
			if err != nil {
				res.Output, res.Code = err.Error(), proxy.Classify(err)
			} else {
				res.IP, res.Latency, res.Country, res.Org = info.IP, info.Latency, info.Country, info.Org
				res.OK = info.IP == ip.String()
				res.Output = info.IP
				if !res.OK {
					res.Output = fmt.Sprintf("expected %s but target saw %s", ip, info.IP)
					res.Code = "wrong_ip"
				}
			}
		}
		<-a.testSem
	case "test_v6":
		info, err := CheckIP(netip.Addr{}, 0, 10*time.Second)
		if err != nil {
			res.Output, res.Code = err.Error(), proxy.Classify(err)
		} else {
			res.OK, res.Output, res.IP, res.Latency, res.Country, res.Org = true, info.IP, info.IP, info.Latency, info.Country, info.Org
		}
	case "restart":
		if !a.cfg.AllowUpdate {
			res.Output = "restart the panel instead"
			break
		}
		res.OK, res.Output = true, "restarting"
		log.Printf("agent: restart requested from the panel")
		a.mu.Lock()
		a.restartPending = true
		a.mu.Unlock()
	case "reapply":
		res.OK, res.Output = true, "network configuration will be re-applied"
		a.mu.Lock()
		a.reapply = true
		a.mu.Unlock()
	case "update":
		if !a.cfg.AllowUpdate {
			res.Output = "this agent is updated together with the panel"
			break
		}
		if err := selfUpdate(c.URL, c.Version, a.cfg.InsecureTLS); err != nil {
			res.Output = err.Error()
			log.Printf("agent: update failed: %v", err)
		} else {
			res.OK, res.Output = true, "updated to "+c.Version+", restarting"
			log.Printf("agent: updated to %s", c.Version)
			a.mu.Lock()
			a.restartPending = true
			a.mu.Unlock()
		}
	default:
		res.Output = "unknown command"
	}
	a.mu.Lock()
	a.results = append(a.results, res)
	delete(a.running, c.ID)
	a.mu.Unlock()
}

// IPInfo is what an external echo service saw.
type IPInfo struct {
	IP      string
	Country string
	Org     string // e.g. "AS18403 FPT Telecom Company"
	Latency int    // ms for the whole request
}

// CheckIP asks IPv6-only echo services which address they see. If src is
// valid the connection is bound to it.
func CheckIP(src netip.Addr, bindIf int, timeout time.Duration) (IPInfo, error) {
	var local net.Addr
	if src.IsValid() {
		local = &net.TCPAddr{IP: src.AsSlice()}
	}
	d := proxy.BindDialer(local, bindIf)
	d.Timeout = timeout
	tr := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp6", addr)
		},
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: timeout}
	var lastErr error
	// ipinfo gives country + ASN; ipify is a plain fallback
	start := time.Now()
	if res, err := cl.Get("https://v6.ipinfo.io/json"); err == nil {
		var j struct{ IP, Country, Org string }
		derr := json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&j)
		res.Body.Close()
		if ip, err := netip.ParseAddr(j.IP); derr == nil && res.StatusCode == 200 && err == nil {
			return IPInfo{IP: ip.String(), Country: j.Country, Org: j.Org, Latency: int(time.Since(start).Milliseconds())}, nil
		}
		lastErr = fmt.Errorf("ipinfo: %s", res.Status)
	} else {
		lastErr = err
	}
	for _, u := range []string{"https://api6.ipify.org", "https://ipv6.icanhazip.com"} {
		start = time.Now()
		res, err := cl.Get(u)
		if err != nil {
			lastErr = err
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 128))
		res.Body.Close()
		if res.StatusCode == 200 {
			if ip, err := netip.ParseAddr(strings.TrimSpace(string(b))); err == nil {
				return IPInfo{IP: ip.String(), Latency: int(time.Since(start).Milliseconds())}, nil
			}
		}
		lastErr = fmt.Errorf("%s: %s", u, res.Status)
	}
	return IPInfo{}, lastErr
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

func selfUpdate(url, version string, insecure bool) error {
	if url == "" {
		return fmt.Errorf("no download URL")
	}
	dst, err := update.SelfPath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tmp, err := update.Download(ctx, url, dst, insecure)
	if err != nil {
		return err
	}
	return update.Install(tmp, dst, version)
}
