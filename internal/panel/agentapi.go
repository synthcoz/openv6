package panel

import (
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

const onlineWindow = 30 // seconds without sync before a node is "offline"

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
	Created time.Time `json:"created"`
}

func (s *Server) queueCommand(nodeID int64, typ string, proxyID int64) *commandState {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	id := store.RandomToken(8)
	c := proto.Command{ID: id, Type: typ, ProxyID: proxyID}
	s.pending[nodeID] = append(s.pending[nodeID], pendingCmd{Command: c, At: time.Now()})
	st := &commandState{ID: id, NodeID: nodeID, Type: typ, ProxyID: proxyID, Created: time.Now()}
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
		logErr(w, err)
		return
	}
	// command results
	s.cmdMu.Lock()
	for _, res := range req.Results {
		if st, ok := s.results[res.ID]; ok && st.NodeID == node.ID {
			st.Done, st.OK, st.Output = true, res.OK, res.Output
			if st.Type == "test_proxy" && st.ProxyID > 0 {
				prefix := "FAIL "
				if res.OK {
					prefix = "OK "
				}
				_ = s.Store.SetCheck(st.ProxyID, prefix+res.Output)
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

	// persist node state (without stats)
	req.Stats, req.Results = nil, nil
	b, _ := json.Marshal(req)
	_ = s.Store.TouchNode(node.ID, string(b))
	s.stateMu.Lock()
	s.live[node.ID] = &liveState{At: time.Now(), Req: req, Active: active, Applied: req.Applied}
	s.stateMu.Unlock()
	if wasOffline {
		s.Store.AddEvent(node.ID, "info", fmt.Sprintf("node %q online (%s, agent %s)", node.Name, req.Hostname, req.AgentVersion))
	}

	// auto pools follow the interface's current /64
	iface := node.Iface
	if iface == "" {
		iface = req.Net.DefaultIface
	}
	if pfx := firstPrefix(req.Net, iface); pfx != "" {
		pools, _ := s.Store.ListPools(node.ID)
		for _, p := range pools {
			if p.Auto && p.CIDR != pfx {
				if p.CIDR != "" {
					s.Store.AddEvent(node.ID, "warn", fmt.Sprintf("node %q: IPv6 prefix changed %s -> %s, proxies re-addressed", node.Name, p.CIDR, pfx))
				}
				_ = s.Store.SetPoolCIDR(p.ID, pfx)
			}
		}
	}

	resp, err := s.desiredState(node)
	if err != nil {
		logErr(w, err)
		return
	}
	resp.Commands = cmds
	writeJSON(w, 200, resp)
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
		Pools: []proto.PoolSpec{}, Proxies: []proto.ProxySpec{}, Commands: []proto.Command{}}
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
	for _, p := range proxies {
		if !p.Enabled || (p.ExpiresAt > 0 && p.ExpiresAt < now) {
			continue
		}
		spec := proto.ProxySpec{ID: p.ID, Port: p.Port, Protocol: p.Protocol, User: p.AuthUser, Pass: p.AuthPass,
			PoolID: p.PoolID, IPv4Fallback: p.IPv4Fallback}
		if ip, err := ProxyIP(p, pm[p.PoolID]); err == nil {
			spec.IP = ip.String()
		}
		resp.Proxies = append(resp.Proxies, spec)
	}
	return resp, nil
}
