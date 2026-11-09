package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"openv6/internal/proto"
	"openv6/internal/store"
)

// ---- sharing proxies over the Internet: relay through a public node ----

func nodeReq(n *store.Node) proto.SyncRequest {
	var req proto.SyncRequest
	_ = json.Unmarshal([]byte(n.StateJSON), &req)
	return req
}

// relayToken authenticates a NAT'ed node at a relay node. It changes when
// either node token is rotated.
func relayToken(client, server *store.Node) string {
	h := sha256.Sum256([]byte(client.Token + "|" + server.Token))
	return hex.EncodeToString(h[:])[:40]
}

// relayHost is the public address of a relay node.
func relayHost(n *store.Node, st proto.NetState) string {
	if n.EntryHost != "" {
		return n.EntryHost
	}
	if st.PublicV4 != "" {
		return st.PublicV4
	}
	return entryHost(n, st)
}

func relayPub(p *store.Proxy) int {
	if p.RelayPort > 0 {
		return p.RelayPort
	}
	return p.Port
}

func proxyActive(p *store.Proxy, now int64) bool {
	return p.Enabled && (p.ExpiresAt == 0 || p.ExpiresAt >= now)
}

// addRelaySpecs fills the relay part of a node's desired state.
func (s *Server) addRelaySpecs(node *store.Node, resp *proto.SyncResponse, own []*store.Proxy) error {
	nodes, err := s.Store.ListNodes()
	if err != nil {
		return err
	}
	nm := map[int64]*store.Node{}
	for _, n := range nodes {
		nm[n.ID] = n
	}
	now := time.Now().Unix()
	// client side: proxies of this node published through relay nodes
	ports := map[int64][]int{}
	var order []int64
	for _, p := range own {
		if node.Paused || !proxyActive(p, now) || p.RelayNodeID == 0 || p.RelayNodeID == node.ID {
			continue
		}
		if _, ok := ports[p.RelayNodeID]; !ok {
			order = append(order, p.RelayNodeID)
		}
		ports[p.RelayNodeID] = append(ports[p.RelayNodeID], p.Port)
	}
	for _, rid := range order {
		rn := nm[rid]
		if rn == nil || rn.RelayPort == 0 {
			continue
		}
		st := nodeReq(rn)
		if st.RelayFP == "" { // relay not started yet; retried on the next sync
			continue
		}
		resp.RelayClients = append(resp.RelayClients, proto.RelayClientSpec{ServerNodeID: rid,
			Addr: net.JoinHostPort(relayHost(rn, st.Net), strconv.Itoa(rn.RelayPort)), Token: relayToken(node, rn),
			Fingerprint: st.RelayFP, Ports: ports[rid]})
	}
	// server side: this node relays proxies of other nodes
	if node.RelayPort > 0 {
		spec := &proto.RelayServerSpec{Port: node.RelayPort, Clients: []proto.RelayAuth{}}
		all, err := s.Store.ListProxies(store.ProxyFilter{RelayNodeID: node.ID})
		if err != nil {
			return err
		}
		idx := map[int64]int{}
		for _, p := range all {
			cn := nm[p.NodeID]
			if cn == nil || cn.ID == node.ID || cn.Paused || !proxyActive(p, now) {
				continue
			}
			i, ok := idx[cn.ID]
			if !ok {
				i = len(spec.Clients)
				idx[cn.ID] = i
				spec.Clients = append(spec.Clients, proto.RelayAuth{NodeID: cn.ID, Token: relayToken(cn, node)})
			}
			spec.Clients[i].Maps = append(spec.Clients[i].Maps, proto.RelayMap{Public: relayPub(p), Target: p.Port})
		}
		resp.RelayServer = spec
	}
	return nil
}

// usedPorts lists every port taken on a node: its proxies, its relay
// listener and ports it opens for relayed proxies.
func (s *Server) usedPorts(nodeID int64, exceptProxy int64) (map[int]bool, error) {
	used := map[int]bool{}
	own, err := s.Store.ListProxies(store.ProxyFilter{NodeID: nodeID})
	if err != nil {
		return nil, err
	}
	for _, p := range own {
		if p.ID != exceptProxy {
			used[p.Port] = true
		}
	}
	relayed, err := s.Store.ListProxies(store.ProxyFilter{RelayNodeID: nodeID})
	if err != nil {
		return nil, err
	}
	for _, p := range relayed {
		if p.ID != exceptProxy && p.NodeID != nodeID {
			used[relayPub(p)] = true
		}
	}
	if n, err := s.Store.GetNode(nodeID); err == nil && n.RelayPort > 0 {
		used[n.RelayPort] = true
	}
	return used, nil
}

// setRelay validates and assigns a relay node (0 = off) for a proxy.
// pub is the wanted public port (0 = same as the proxy port, or the next free one).
func (s *Server) setRelay(p *store.Proxy, relayID int64, pub int, used map[int]bool) error {
	if relayID == 0 {
		p.RelayNodeID, p.RelayPort = 0, 0
		return nil
	}
	rn, err := s.Store.GetNode(relayID)
	if err != nil {
		return errors.New("relay node not found")
	}
	if rn.ID == p.NodeID {
		return errors.New("a node cannot relay its own proxies; share them directly (scope Internet)")
	}
	if rn.RelayPort == 0 {
		return fmt.Errorf("node %q is not a relay yet: turn on \"Relay for other nodes\" in its settings", rn.Name)
	}
	if p.AuthUser == "" {
		return fmt.Errorf("proxy :%d must have a username and password to be shared through a relay", p.Port)
	}
	if used == nil {
		if used, err = s.usedPorts(rn.ID, p.ID); err != nil {
			return err
		}
	}
	if pub == 0 && p.RelayNodeID == relayID && p.RelayPort > 0 && !used[p.RelayPort] {
		pub = p.RelayPort
	}
	if pub > 0 {
		if used[pub] || s.reservedPort(pub) {
			return fmt.Errorf("port %d is already used on %q", pub, rn.Name)
		}
	} else {
		pub = p.Port
		for used[pub] || s.reservedPort(pub) {
			pub++
			if pub > 65535 {
				return fmt.Errorf("no free ports left on %q", rn.Name)
			}
		}
	}
	used[pub] = true
	p.RelayNodeID, p.RelayPort = relayID, pub
	return nil
}

// ---- API tokens ----

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := s.Store.ListAPITokens()
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, ts)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		fail(w, 403, "API tokens cannot create API tokens")
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" {
		b.Name = "API"
	}
	tok, t, err := s.Store.CreateAPIToken(b.Name)
	if err != nil {
		logErr(w, err)
		return
	}
	s.Store.AddEvent(0, "info", fmt.Sprintf("API token %q created", b.Name))
	writeJSON(w, 200, map[string]any{"token": tok, "info": t})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		fail(w, 403, "API tokens cannot delete API tokens")
		return
	}
	if err := s.Store.DeleteAPIToken(pathID(r)); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
