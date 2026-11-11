package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"openv6/internal/proto"
	"openv6/internal/store"
	"openv6/internal/v6"
)

const onlineWindow = 45 // seconds without sync before a node is "offline"

type liveState struct {
	At      time.Time
	Req     proto.SyncRequest // Stats/Results stripped
	Active  map[int64]int64
	Applied map[int64]proto.Applied
}

type pendingCmd struct {
	proto.Command
	At time.Time
}

type commandState struct {
	ID      string    `json:"id"`
	NodeID  int64     `json:"node_id"`
	Type    string    `json:"type"`
	ProxyID int64     `json:"proxy_id,omitempty"`
	Done    bool      `json:"done"`
	OK      bool      `json:"ok"`
	Output  string    `json:"output"`
	IP      string    `json:"ip,omitempty"`
	Latency int       `json:"latency,omitempty"`
	Country string    `json:"country,omitempty"`
	Org     string    `json:"org,omitempty"`
	Code    string    `json:"code,omitempty"`
	Created time.Time `json:"created"`
}

func (s *Server) queueCommand(nodeID int64, c proto.Command) *commandState {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	id := store.RandomToken(8)
	c.ID = id
	s.pending[nodeID] = append(s.pending[nodeID], pendingCmd{Command: c, At: time.Now()})
	st := &commandState{ID: id, NodeID: nodeID, Type: c.Type, ProxyID: c.ProxyID, Created: time.Now()}
	s.results[id] = st
	// garbage-collect old results
	for k, v := range s.results {
		if time.Since(v.Created) > 10*time.Minute {
			delete(s.results, k)
		}
	}
	return st
}

func (s *Server) handleGetCommand(w http.ResponseWriter, r *http.Request) {
	s.cmdMu.Lock()
	st, ok := s.results[r.PathValue("id")]
	var cp commandState
	if ok {
		cp = *st
	}
	s.cmdMu.Unlock()
	if !ok {
		fail(w, 404, "unknown command")
		return
	}
	if isCustomer(r) {
		if p, err := s.Store.GetProxy(cp.ProxyID); cp.ProxyID == 0 || err != nil || p.OwnerID != currentUser(r).ID {
			fail(w, 404, "unknown command")
			return
		}
	}
	if !cp.Done && time.Since(cp.Created) > 60*time.Second {
		cp.Done, cp.Output = true, "timeout: node did not answer"
	}
	writeJSON(w, 200, cp)
}

func (s *Server) handleAgentSync(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	node, err := s.Store.NodeByToken(tok)
	if err != nil {
		fail(w, http.StatusUnauthorized, "invalid node token")
		return
	}
	var req proto.SyncRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	resp, err := s.processSync(node, &req)
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, resp)
}

// LocalSync lets the in-process agent of this machine sync without HTTP.
func (s *Server) LocalSync(ctx context.Context, token string, req *proto.SyncRequest) (*proto.SyncResponse, error) {
	node, err := s.Store.NodeByToken(token)
	if err != nil {
		return nil, err
	}
	// round-trip through JSON so the agent never shares memory with the panel
	b, _ := json.Marshal(req)
	var cp proto.SyncRequest
	_ = json.Unmarshal(b, &cp)
	return s.processSync(node, &cp)
}

func (s *Server) processSync(node *store.Node, req *proto.SyncRequest) (*proto.SyncResponse, error) {
	wasOffline := time.Now().Unix()-node.LastSeen > onlineWindow

	// stats
	deltas := map[int64]store.StatDelta{}
	active := map[int64]int64{}
	for id, st := range req.Stats {
		if st.Up != 0 || st.Down != 0 || st.Conns != 0 {
			deltas[id] = store.StatDelta{Up: st.Up, Down: st.Down, Conns: st.Conns}
		}
		active[id] = st.Active
	}
	if err := s.Store.AddStats(node.ID, deltas); err != nil {
		return nil, err
	}
	// command results
	type checkDone struct {
		proxyID int64
		res     proto.CommandResult
	}
	var checks []checkDone
	s.cmdMu.Lock()
	for _, res := range req.Results {
		if st, ok := s.results[res.ID]; ok && st.NodeID == node.ID {
			st.Done, st.OK, st.Output = true, res.OK, res.Output
			st.IP, st.Latency, st.Country, st.Org = res.IP, res.Latency, res.Country, res.Org
			st.Code = res.Code
			if st.Type == "test_proxy" && st.ProxyID > 0 {
				checks = append(checks, checkDone{st.ProxyID, res})
			}
			if st.Type == "update" {
				lvl := "info"
				if !res.OK {
					lvl = "warn"
				}
				s.Store.AddEvent(node.ID, lvl, fmt.Sprintf("node %q update: %s", node.Name, res.Output))
			}
		}
	}
	cmds := []proto.Command{}
	var keep []pendingCmd
	for _, c := range s.pending[node.ID] {
		if time.Since(c.At) < 60*time.Second {
			cmds = append(cmds, c.Command)
		}
	}
	s.pending[node.ID] = keep
	s.cmdMu.Unlock()
	for _, c := range checks {
		s.onCheckResult(node.ID, c.proxyID, c.res)
	}
	if len(req.Logs) > 0 {
		s.nodeLog(node.ID).Append(req.Logs)
	}

	// persist node state (without stats)
	req.Stats, req.Results, req.Logs = nil, nil, nil
	b, _ := json.Marshal(req)
	_ = s.Store.TouchNode(node.ID, string(b))
	s.stateMu.Lock()
	s.live[node.ID] = &liveState{At: time.Now(), Req: *req, Active: active, Applied: req.Applied}
	s.stateMu.Unlock()
	if wasOffline {
		s.Store.AddEvent(node.ID, "info", fmt.Sprintf("node %q online (%s, agent %s)", node.Name, req.Hostname, req.AgentVersion))
	}

	// auto pools follow the interface's current /64
	iface := node.Iface
	if iface == "" {
		iface = req.Net.PrimaryIface
	}
	if iface == "" {
		iface = req.Net.DefaultIface
	}
	if pfx := firstPrefix(req.Net, iface); pfx != "" {
		pools, _ := s.Store.ListPools(node.ID)
		for _, p := range pools {
			if p.Auto && p.CIDR != pfx {
				if p.CIDR != "" {
					s.Store.AddEvent(node.ID, "warn", fmt.Sprintf("node %q: IPv6 prefix changed %s -> %s, proxies re-addressed", node.Name, p.CIDR, pfx))
					s.alert(AlertPrefix, s.msg("prefix_change", node.Name, p.CIDR, pfx))
				}
				_ = s.Store.SetPoolCIDR(p.ID, pfx)
			}
		}
	}

	resp, err := s.desiredState(node)
	if err != nil {
		return nil, err
	}
	resp.Commands = cmds
	return resp, nil
}

// DNS presets selectable per node.
var dnsPresets = map[string][]string{
	"cloudflare": {"2606:4700:4700::1111", "2606:4700:4700::1001", "1.1.1.1"},
	"google":     {"2001:4860:4860::8888", "2001:4860:4860::8844", "8.8.8.8"},
	"quad9":      {"2620:fe::fe", "2620:fe::9", "9.9.9.9"},
}

func dnsServers(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" || v == "system" {
		return nil
	}
	if p, ok := dnsPresets[v]; ok {
		return p
	}
	var out []string
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if a, err := netip.ParseAddr(strings.TrimSpace(f)); err == nil {
			out = append(out, a.String())
		}
	}
	return out
}

func firstPrefix(st proto.NetState, iface string) string {
	for _, it := range st.Interfaces {
		if it.Name == iface && len(it.Prefixes) > 0 {
			return it.Prefixes[0]
		}
	}
	return ""
}

// ProxyIP computes the outbound address of a proxy from its pool.
func ProxyIP(p *store.Proxy, pool *store.Pool) (netip.Addr, error) {
	if pool == nil || pool.CIDR == "" {
		return netip.Addr{}, fmt.Errorf("pool has no prefix yet")
	}
	pfx, err := netip.ParsePrefix(pool.CIDR)
	if err != nil {
		return netip.Addr{}, err
	}
	suf, err := v6.ParseSuffix(p.Suffix)
	if err != nil {
		return netip.Addr{}, err
	}
	return v6.Compose(pfx, p.Subnet, suf)
}

func (s *Server) desiredState(node *store.Node) (*proto.SyncResponse, error) {
	pools, err := s.Store.ListPools(node.ID)
	if err != nil {
		return nil, err
	}
	pm := map[int64]*store.Pool{}
	resp := &proto.SyncResponse{NodeID: node.ID, NodeName: node.Name, Iface: node.Iface, Interval: 5,
		Firewall:  s.Store.GetSetting("auto_firewall", "1") == "1",
		Bypass:    node.Bypass,
		DNS:       dnsServers(node.DNS),
		UPnP:      node.UPnP,
		Block:     s.blockRules(),
		SecureDNS: s.Store.GetSetting("secure_dns", "1") == "1",
		IPv4Sites: func() []string { r, _ := cleanBlockRules(s.Store.GetSetting("ipv4_sites", "")); return r }(),
		NAT64:     s.Store.GetSetting("nat64_prefix", ""),
		IPv4Mode:  s.ipv4Mode(),
		Pools:     []proto.PoolSpec{}, Proxies: []proto.ProxySpec{}, Commands: []proto.Command{}}
	for _, p := range pools {
		pm[p.ID] = p
		resp.Pools = append(resp.Pools, proto.PoolSpec{ID: p.ID, CIDR: p.CIDR, Mode: p.Mode, Auto: p.Auto})
	}
	for _, line := range strings.Split(node.AllowCIDRs, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			resp.AllowCIDRs = append(resp.AllowCIDRs, l)
		}
	}
	proxies, err := s.Store.ListProxies(store.ProxyFilter{NodeID: node.ID})
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	pro := s.pro()
	var freeIDs map[int64]bool
	if !pro {
		_, freeIDs = s.freeAllowed()
		resp.UPnP = false
	}
	for _, p := range proxies {
		if node.Paused || !p.Enabled || (p.ExpiresAt > 0 && p.ExpiresAt < now) {
			continue
		}
		if !pro {
			if !freeIDs[p.ID] {
				continue
			}
			p.MaxConns, p.RateKBps, p.RelayNodeID = 0, 0, 0
		}
		spec := proto.ProxySpec{ID: p.ID, Port: p.Port, Protocol: p.Protocol, User: p.AuthUser, Pass: p.AuthPass,
			PoolID: p.PoolID, IPv4Fallback: p.IPv4Fallback, Scope: p.Scope, MaxConns: p.MaxConns, RateKBps: p.RateKBps}
		spec.Iface = p.Iface
		if ip, err := ProxyIP(p, pm[p.PoolID]); err == nil {
			spec.IP = ip.String()
		}
		resp.Proxies = append(resp.Proxies, spec)
	}
	if pro {
		if err := s.addRelaySpecs(node, resp, proxies); err != nil {
			return nil, err
		}
	}
	return resp, nil
}
