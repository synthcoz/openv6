// Package agent runs on every node: it polls the panel, installs IPv6
// addresses, runs the proxy listeners and reports stats.
package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
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
	"openv6/internal/relay"
	"openv6/internal/update"
	"openv6/internal/upnp"
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
	cfg      Config
	client   *http.Client
	net      *netcfg.Manager
	proxies  *proxy.Manager
	relaySrv *relay.Server
	relayCl  *relay.Client
	upnp     *upnp.Mapper

	mu       sync.Mutex
	lastSent map[int64]proto.ProxyStats
	applied  map[int64]proto.Applied
	results  []proto.CommandResult
	errors   []string
	running  map[string]bool // command ids in progress
	specs    map[int64]proto.ProxySpec
	pcfgs    map[int64]proxy.Config // running proxy configs (for site tests)
	doh      *proxy.DoH             // encrypted DNS shared by all proxies

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

	// apply only when the desired state changes (plus a periodic self-heal),
	// so the 5 s heartbeat stays cheap and never stalls
	lastHash  string
	lastApply time.Time
	lastOK    bool
	force     bool // apply again without wiping addresses (after a fix)
	netCache  proto.NetState
	netAt     time.Time
	netIface  string
}

// resyncEvery re-applies the configuration even when nothing changed, to
// repair addresses or listeners the OS dropped (network change, sleep).
const resyncEvery = 60 * time.Second

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
		doh:       proxy.NewDoH(),
		relaySrv:  relay.NewServer(cfg.DataDir),
		relayCl:   relay.NewClient(),
		upnp:      upnp.New(),
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
			a.maybeApply(resp)
			a.mu.Lock()
			restart := a.restartPending && len(a.results) == 0
			a.mu.Unlock()
			if restart {
				log.Printf("agent: restarting into the updated binary")
				a.proxies.Close()
				os.Exit(3) // non-zero: systemd, launchd and Windows recovery all restart it
			}
			if resp.Interval > 0 {
				interval = time.Duration(resp.Interval) * time.Second
			}
		}
		select {
		case <-ctx.Done():
			a.proxies.Close()
			a.relaySrv.Close()
			a.relayCl.Close()
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
	st := a.detectCached(cfgIface)
	st.LANv4 = lanIPv4()
	st.PublicV4 = a.publicV4()
	st.TailscaleIP = tailscaleIP()

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
		RelayFP:   a.relaySrv.Fingerprint(), Relays: a.relayCl.Status(), UPnP: a.upnp.Status(),
	}
	for _, id := range a.relaySrv.Connected() {
		req.Relays = append(req.Relays, proto.RelayState{Role: "server", PeerNodeID: id, Connected: true})
	}
	var out proto.SyncResponse
	if a.cfg.Direct != nil {
		dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		resp, err := a.cfg.Direct(dctx, &req)
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
	want := netcfg.Desired{Iface: iface, Addrs: map[netip.Addr]bool{}, Routes: map[string]bool{}, On: map[netip.Addr]string{}}
	hasIf := map[string]bool{}
	for _, it := range st.Interfaces {
		hasIf[it.Name] = true
	}
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
	block := proxy.ParseBlocklist(r.Block)
	var doh *proxy.DoH
	if r.SecureDNS {
		doh = a.doh
	}
	for _, s := range r.Proxies {
		specs[s.ID] = s
		ip, err := netip.ParseAddr(s.IP)
		if err != nil {
			applied[s.ID] = proto.Applied{IP: s.IP, Error: "no IPv6 prefix available yet", Code: "no_prefix"}
			continue
		}
		pBind := bindIf
		other := s.Iface != "" && s.Iface != iface // this proxy uses another network card
		if other {
			if !hasIf[s.Iface] {
				applied[s.ID] = proto.Applied{IP: s.IP, Error: "network card " + s.Iface + " is not connected or has no IPv6", Code: "iface_missing"}
				continue
			}
			pBind = netcfg.IfIndex(s.Iface)
		}
		if p, ok := pools[s.PoolID]; !ok || p.Mode != "anyip" {
			want.Addrs[ip] = true
			if other {
				want.On[ip] = s.Iface
			} else {
				bypass.Sources = append(bypass.Sources, ip.String()+"/128")
			}
		}
		c := proxy.Config{ID: s.ID, Port: s.Port, Protocol: s.Protocol, User: s.User, Pass: s.Pass, OutIP: ip,
			IPv4Fallback: s.IPv4Fallback, Scope: s.Scope, BindIf: pBind, MaxConns: s.MaxConns, RateKBps: s.RateKBps,
			Block: block, DoH: doh}
		if len(r.DNS) > 0 {
			c.Resolver = proxy.NewResolver(r.DNS, ip, pBind)
		}
		cfgs = append(cfgs, c)
	}
	var addrErrs map[netip.Addr]error
	var errs []error
	if iface == "" && len(want.Addrs) > 0 {
		// interface gone for a moment (resume, Wi-Fi reconnect): keep the
		// addresses we have and report every proxy as waiting, so the next
		// tick retries instead of wiping everything and claiming success
		addrErrs = map[netip.Addr]error{}
		for ip := range want.Addrs {
			addrErrs[ip] = fmt.Errorf("no network card with IPv6 right now; retrying")
		}
		errs = append(errs, fmt.Errorf("no IPv6 network card found (cable unplugged or Wi-Fi reconnecting?)"))
	} else {
		addrErrs, errs = a.net.Apply(want)
	}
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
	pcfgs := map[int64]proxy.Config{}
	for _, c := range ok {
		pcfgs[c.ID] = c
	}
	// sharing over the Internet: relay (public node <-> NAT'ed node) and UPnP
	for _, e := range a.relaySrv.Apply(r.RelayServer) {
		errs = append(errs, fmt.Errorf("%s", e))
	}
	a.relayCl.Apply(r.RelayClients)
	var anyPorts []int
	for _, c := range ok {
		if c.Scope == "any" {
			anyPorts = append(anyPorts, c.Port)
		}
	}
	if !a.cfg.DryRun {
		a.upnp.Apply(r.UPnP, anyPorts)
	}
	if r.Firewall {
		var ports []int
		for _, c := range ok {
			if c.Scope != "local" {
				ports = append(ports, c.Port)
			}
		}
		if rs := r.RelayServer; rs != nil && rs.Port > 0 {
			ports = append(ports, rs.Port)
			for _, cl := range rs.Clients {
				for _, m := range cl.Maps {
					ports = append(ports, m.Public)
				}
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
	allOK := len(errs) == 0
	for _, ap := range applied {
		if !ap.OK {
			allOK = false
		}
	}
	a.mu.Lock()
	a.lastOK = allOK
	a.fwManaged = r.Firewall
	a.bypassStatus, a.bypassErr = bypassStatus, bypassErr
	a.applied = applied
	a.errors = errStrs
	a.specs = specs
	a.pcfgs = pcfgs
	a.bindIf = bindIf
	a.netCache, a.netAt, a.netIface = st, time.Now(), r.Iface
	a.mu.Unlock()
	a.runCommands(r.Commands)
}

// maybeApply applies the desired state when it changed, when a re-apply was
// requested, or every resyncEvery; otherwise it only starts new commands.
func (a *Agent) maybeApply(r *proto.SyncResponse) {
	cp := *r
	cp.Commands, cp.Interval = nil, 0
	b, _ := json.Marshal(cp)
	sum := sha256.Sum256(b)
	h := hex.EncodeToString(sum[:])
	// local facts the panel does not know about: which card is primary, its
	// index (changes when an adapter resets), the router, vanished addresses
	st := a.detectCached(r.Iface)
	local := fmt.Sprintf("%s|%d|%s|%d", st.PrimaryIface, netcfg.IfIndex(st.PrimaryIface), st.Gateway, a.net.MissingManaged())
	seen := map[string]bool{}
	for _, p := range r.Proxies { // cards used by single proxies (Wi-Fi re-plugged = new index)
		if p.Iface != "" && !seen[p.Iface] {
			seen[p.Iface] = true
			local += fmt.Sprintf("|%s:%d", p.Iface, netcfg.IfIndex(p.Iface))
		}
	}
	sum2 := sha256.Sum256([]byte(h + local))
	h = hex.EncodeToString(sum2[:])
	a.mu.Lock()
	force := a.reapply || a.force
	a.force = false
	lastOK := a.lastOK
	a.mu.Unlock()
	due := time.Since(a.lastApply) >= resyncEvery || (!lastOK && time.Since(a.lastApply) >= 15*time.Second)
	if !force && h == a.lastHash && !due {
		a.mu.Lock()
		a.iface = r.Iface
		a.mu.Unlock()
		a.runCommands(r.Commands)
		return
	}
	start := time.Now()
	a.apply(r)
	a.lastHash, a.lastApply = h, time.Now()
	if d := time.Since(start); d > 10*time.Second {
		log.Printf("agent: applying the configuration took %s", d.Round(time.Second))
	}
}

func (a *Agent) runCommands(cmds []proto.Command) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range cmds {
		if a.running[c.ID] {
			continue
		}
		a.running[c.ID] = true
		go a.runCommand(c)
	}
}

// detectCached returns the network state, re-detecting at most every 15 s
// (detection runs system tools on Windows and macOS).
func (a *Agent) detectCached(iface string) proto.NetState {
	a.mu.Lock()
	st, at, same := a.netCache, a.netAt, a.netIface == iface
	a.mu.Unlock()
	if same && !at.IsZero() && time.Since(at) < 10*time.Second {
		return st
	}
	st = a.net.Detect(iface)
	a.mu.Lock()
	a.netCache, a.netAt, a.netIface = st, time.Now(), iface
	a.mu.Unlock()
	return st
}

func (a *Agent) runCommand(c proto.Command) {
	res := proto.CommandResult{ID: c.ID}
	restart := false
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
	case "test_site": // why does (or doesn't) a website open through this proxy?
		a.testSem <- struct{}{}
		a.mu.Lock()
		cfg, ok := a.pcfgs[c.ProxyID]
		a.mu.Unlock()
		if !ok {
			res.Output, res.Code = "proxy not running on this node", "not_running"
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			rep := proxy.Diagnose(ctx, &cfg, net.DefaultResolver, a.doh, c.Host)
			cancel()
			b, _ := json.Marshal(rep)
			res.OK, res.Output, res.Code = rep.Verdict == "ok", string(b), rep.Verdict
			log.Printf("agent: site test %s via proxy %d: %s", rep.Host, c.ProxyID, rep.Verdict)
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
		restart = true
	case "fix":
		out, err := a.net.Fix(c.Fix)
		if err != nil {
			res.Output = err.Error()
		} else {
			res.OK, res.Output = true, out
			if c.Fix == "kernel_tuning" {
				a.mu.Lock()
				a.tuneFailed = nil
				a.mu.Unlock()
			}
			a.mu.Lock()
			a.force = true
			a.netAt = time.Time{}
			a.mu.Unlock()
		}
		log.Printf("agent: fix %s: ok=%v %s", c.Fix, res.OK, res.Output)
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
			restart = true
		}
	default:
		res.Output = "unknown command"
	}
	a.mu.Lock()
	a.results = append(a.results, res)
	delete(a.running, c.ID)
	if restart { // same critical section: the result is queued before Run may exit
		a.restartPending = true
	}
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

// tailscaleIP returns this machine's Tailscale IPv4 (100.64.0.0/10), if any.
func tailscaleIP() string {
	cg := netip.MustParsePrefix("100.64.0.0/10")
	ifs, _ := net.Interfaces()
	fallback := ""
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(it.Name)
		addrs, _ := it.Addrs()
		for _, ad := range addrs {
			ipn, ok := ad.(*net.IPNet)
			if !ok {
				continue
			}
			a, ok := netip.AddrFromSlice(ipn.IP)
			if !ok || !a.Unmap().Is4() || !cg.Contains(a.Unmap()) {
				continue
			}
			if strings.Contains(name, "tailscale") {
				return a.Unmap().String()
			}
			if strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tun") {
				fallback = a.Unmap().String()
			}
		}
	}
	return fallback
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
	// the panel (or anyone on the path to it) must not be able to push an
	// arbitrary binary: check it against the release's signed checksums
	base := url
	if i := strings.Index(base, "/download/"); i >= 0 {
		base = base[:i]
	}
	read := func(name string) ([]byte, error) {
		p, err := update.Download(ctx, base+"/download/"+name, dst, insecure)
		if err != nil {
			return nil, err
		}
		defer os.Remove(p)
		return os.ReadFile(p)
	}
	sums, err1 := read("SHA256SUMS")
	sig, err2 := read("SHA256SUMS.sig")
	if err1 != nil || err2 != nil {
		os.Remove(tmp)
		return fmt.Errorf("panel has no signed checksums: update refused")
	}
	m, err := update.VerifySums(sums, sig, version)
	if err == nil {
		err = update.VerifyFile(tmp, m[update.AssetName(runtime.GOOS, runtime.GOARCH)])
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return update.Install(tmp, dst, version)
}
