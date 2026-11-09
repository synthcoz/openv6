package panel

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"openv6/internal/license"
	"openv6/internal/proto"
	"openv6/internal/store"
	"openv6/internal/v6"
)

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// ---- nodes ----

type nodeView struct {
	*store.Node
	Online   bool                `json:"online"`
	Hostname string              `json:"hostname"`
	OS       string              `json:"os"`
	Version  string              `json:"agent_version"`
	Net      proto.NetState      `json:"net"`
	Errors   []string            `json:"errors"`
	Proxies  int                 `json:"proxies"`
	Pools    []*store.Pool       `json:"pools"`
	Entry    string              `json:"entry"` // effective entry host
	Checks   []proto.SystemCheck `json:"checks"`
	Uptime   int64               `json:"uptime"` // seconds since the agent started
	Outdated bool                `json:"outdated"`
	Extra    map[string]string   `json:"extra,omitempty"`
	Relays   []proto.RelayState  `json:"relays"`
	UPnPSt   *proto.UPnPState    `json:"upnp_state,omitempty"`
	RelayFP  string              `json:"relay_fp,omitempty"`
}

func (s *Server) nodeView(n *store.Node, proxyCounts map[int64]int) nodeView {
	v := nodeView{Node: n, Online: time.Now().Unix()-n.LastSeen <= onlineWindow, Proxies: proxyCounts[n.ID], Errors: []string{}}
	var req proto.SyncRequest
	_ = json.Unmarshal([]byte(n.StateJSON), &req)
	v.Hostname, v.OS, v.Version, v.Net = req.Hostname, req.OS, req.AgentVersion, req.Net
	v.Checks = req.Checks
	v.Relays, v.UPnPSt, v.RelayFP = req.Relays, req.UPnP, req.RelayFP
	if v.Relays == nil {
		v.Relays = []proto.RelayState{}
	}
	if req.StartedAt > 0 && v.Online {
		v.Uptime = time.Now().Unix() - req.StartedAt
	}
	if v.Checks == nil {
		v.Checks = []proto.SystemCheck{}
	}
	v.Outdated = !n.IsLocal && req.AgentVersion != "" && req.AgentVersion != proto.Version
	if req.Errors != nil {
		v.Errors = req.Errors
	}
	v.Pools, _ = s.Store.ListPools(n.ID)
	v.Entry = entryHost(n, req.Net)
	v.Token = "" // never list tokens; fetched through /install
	return v
}

// entryHost is the address clients use to reach the node's proxies.
func entryHost(n *store.Node, st proto.NetState) string {
	if n.EntryHost != "" {
		return n.EntryHost
	}
	if n.IsLocal {
		for _, ip := range st.LANv4 {
			if a, err := netip.ParseAddr(ip); err == nil && a.IsPrivate() {
				return ip
			}
		}
	}
	if st.PublicV4 != "" {
		return st.PublicV4
	}
	if len(st.LANv4) > 0 {
		return st.LANv4[0]
	}
	return "127.0.0.1"
}

func (s *Server) proxyCounts() map[int64]int {
	m := map[int64]int{}
	rows, err := s.Store.DB.Query(`SELECT node_id, COUNT(*) FROM proxies GROUP BY node_id`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var c int
		_ = rows.Scan(&id, &c)
		m[id] = c
	}
	return m
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.Store.ListNodes()
	if err != nil {
		logErr(w, err)
		return
	}
	pc := s.proxyCounts()
	out := []nodeView{}
	for _, n := range nodes {
		out = append(out, s.nodeView(n, pc))
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name      string `json:"name"`
		EntryHost string `json:"entry_host"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		fail(w, 400, "name required")
		return
	}
	if nodes, _ := s.Store.ListNodes(); len(nodes) >= 1 && s.needPro(w, "More than one machine (node)") {
		return
	}
	n, err := s.Store.CreateNode(strings.TrimSpace(b.Name), strings.TrimSpace(b.EntryHost), false)
	if err != nil {
		logErr(w, err)
		return
	}
	s.Store.AddEvent(n.ID, "info", fmt.Sprintf("node %q created", n.Name))
	writeJSON(w, 200, s.nodeView(n, nil))
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	var b struct {
		Name       *string `json:"name"`
		EntryHost  *string `json:"entry_host"`
		Iface      *string `json:"iface"`
		AllowCIDRs *string `json:"allow_cidrs"`
		Bypass     *bool   `json:"bypass"`
		DNS        *string `json:"dns"`
		RelayPort  *int    `json:"relay_port"`
		UPnP       *bool   `json:"upnp"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
		n.Name = strings.TrimSpace(*b.Name)
	}
	if b.EntryHost != nil {
		n.EntryHost = strings.TrimSpace(*b.EntryHost)
	}
	if b.Iface != nil {
		n.Iface = strings.TrimSpace(*b.Iface)
	}
	if b.Bypass != nil {
		n.Bypass = *b.Bypass
	}
	if ((b.UPnP != nil && *b.UPnP) || (b.RelayPort != nil && *b.RelayPort > 0)) && s.needPro(w, "Sharing to the Internet (UPnP, relay)") {
		return
	}
	if b.UPnP != nil {
		n.UPnP = *b.UPnP
	}
	if b.RelayPort != nil && *b.RelayPort != n.RelayPort {
		rp := *b.RelayPort
		if rp != 0 {
			used, err := s.usedPorts(n.ID, 0)
			if err != nil {
				logErr(w, err)
				return
			}
			if s.reservedPort(rp) || used[rp] {
				fail(w, 400, fmt.Sprintf("relay port %d is not allowed or already used on this node", rp))
				return
			}
		}
		n.RelayPort = rp
	}
	if b.DNS != nil {
		v := strings.TrimSpace(*b.DNS)
		if _, preset := dnsPresets[v]; !preset && v != "" && v != "system" && len(dnsServers(v)) == 0 {
			fail(w, 400, "DNS must be system, cloudflare, google, quad9 or a list of IP addresses")
			return
		}
		n.DNS = v
	}
	if b.AllowCIDRs != nil {
		var lines []string
		for _, l := range strings.FieldsFunc(*b.AllowCIDRs, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' }) {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if _, err := netip.ParsePrefix(l); err != nil {
				if _, err2 := netip.ParseAddr(l); err2 != nil {
					fail(w, 400, "invalid CIDR/IP: "+l)
					return
				}
			}
			lines = append(lines, l)
		}
		n.AllowCIDRs = strings.Join(lines, "\n")
	}
	if err := s.Store.UpdateNode(n); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, s.nodeView(n, s.proxyCounts()))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	if n.IsLocal {
		fail(w, 400, "the local node cannot be deleted")
		return
	}
	if err := s.Store.DeleteNode(n.ID); err != nil {
		logErr(w, err)
		return
	}
	_, _ = s.Store.DB.Exec(`UPDATE proxies SET relay_node_id=0, relay_port=0 WHERE relay_node_id=?`, n.ID)
	s.Store.AddEvent(0, "info", fmt.Sprintf("node %q deleted", n.Name))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil || n.IsLocal {
		fail(w, 400, "cannot rotate token of this node")
		return
	}
	if _, err := s.Store.RotateNodeToken(n.ID); err != nil {
		logErr(w, err)
		return
	}
	s.Store.AddEvent(n.ID, "warn", fmt.Sprintf("node %q token rotated; re-run the install command on that machine", n.Name))
	s.handleInstallCmd(w, r)
}

func (s *Server) handleTestNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	writeJSON(w, 200, s.queueCommand(n.ID, proto.Command{Type: "test_v6"}))
}

func (s *Server) panelURL(r *http.Request) string {
	if u := s.Store.GetSetting("panel_url", ""); u != "" {
		return strings.TrimRight(u, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleInstallCmd(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	if n.IsLocal {
		writeJSON(w, 200, map[string]string{"command": "", "note": "local node runs inside the panel process"})
		return
	}
	pu := s.panelURL(r)
	cmd := fmt.Sprintf("curl -fsSL %s/install.sh | sudo bash -s -- agent --panel %s --token %s", pu, pu, n.Token)
	win := fmt.Sprintf("& ([scriptblock]::Create((irm %s/install.ps1))) -Token %s", pu, n.Token)
	mac := fmt.Sprintf("curl -fsSL -o /tmp/openv6 %s/download/darwin/$(uname -m | sed s/x86_64/amd64/) && chmod +x /tmp/openv6 && sudo /tmp/openv6 install agent -panel %s -token %s", pu, pu, n.Token)
	writeJSON(w, 200, map[string]string{"command": cmd, "command_windows": win, "command_macos": mac, "panel_url": pu, "token": n.Token})
}

// ---- pools ----

func (s *Server) handleListPools(w http.ResponseWriter, r *http.Request) {
	nid, _ := strconv.ParseInt(r.URL.Query().Get("node_id"), 10, 64)
	ps, err := s.Store.ListPools(nid)
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, ps)
}

func validatePool(p *store.Pool) error {
	pfx, err := netip.ParsePrefix(strings.TrimSpace(p.CIDR))
	if err != nil || !pfx.Addr().Is6() {
		return errors.New("CIDR must be an IPv6 prefix like 2a01:4f8:1:2::/64")
	}
	if pfx.Bits() > 64 || pfx.Bits() < 32 {
		return errors.New("prefix length must be between /32 and /64")
	}
	p.CIDR = pfx.Masked().String()
	if p.Mode != "addr" && p.Mode != "anyip" {
		return errors.New("mode must be addr or anyip")
	}
	if p.Mode == "addr" && pfx.Bits() != 64 {
		return errors.New("addr mode only supports a /64 on-link prefix; use anyip for routed /48 or /56")
	}
	if strings.TrimSpace(p.Name) == "" {
		p.Name = p.CIDR
	}
	return nil
}

func (s *Server) handleCreatePool(w http.ResponseWriter, r *http.Request) {
	var p store.Pool
	if err := readJSON(r, &p); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if _, err := s.Store.GetNode(p.NodeID); err != nil {
		fail(w, 400, "node not found")
		return
	}
	if err := validatePool(&p); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := s.Store.CreatePool(&p); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleUpdatePool(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.GetPool(pathID(r))
	if err != nil {
		fail(w, 404, "pool not found")
		return
	}
	var b store.Pool
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if p.Auto {
		p.Name = b.Name
	} else {
		oldBits := netip.MustParsePrefix(p.CIDR).Bits()
		p.Name, p.CIDR, p.Mode = b.Name, b.CIDR, b.Mode
		if err := validatePool(p); err != nil {
			fail(w, 400, err.Error())
			return
		}
		if netip.MustParsePrefix(p.CIDR).Bits() != oldBits {
			fail(w, 400, "prefix length cannot change once proxies may use this pool; create a new pool instead")
			return
		}
	}
	if err := s.Store.UpdatePool(p); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleDeletePool(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.GetPool(pathID(r))
	if err != nil || p.Auto {
		fail(w, 400, "this pool cannot be deleted")
		return
	}
	var n int
	_ = s.Store.DB.QueryRow(`SELECT COUNT(*) FROM proxies WHERE pool_id=?`, p.ID).Scan(&n)
	if n > 0 {
		fail(w, 400, fmt.Sprintf("pool is used by %d proxies; delete or move them first", n))
		return
	}
	if err := s.Store.DeletePool(p.ID); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- proxies ----

type proxyView struct {
	*store.Proxy
	IP       string `json:"ip"`
	NodeName string `json:"node_name"`
	PoolName string `json:"pool_name"`
	Host     string `json:"host"`   // entry host clients connect to
	Status   string `json:"status"` // ok | error | offline | disabled | expired | pending
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
	Active   int64  `json:"active"`
	Subnet64 string `json:"subnet64"`
	// other ways to reach the proxy
	RelayName string `json:"relay_name,omitempty"`
	RelayHost string `json:"relay_host,omitempty"`
	RelayUp   bool   `json:"relay_up,omitempty"`
	TSHost    string `json:"ts_host,omitempty"`
	WANHost   string `json:"wan_host,omitempty"`
}

func (s *Server) proxyViews(f store.ProxyFilter) ([]proxyView, error) {
	ps, err := s.Store.ListProxies(f)
	if err != nil {
		return nil, err
	}
	nodes, _ := s.Store.ListNodes()
	nm := map[int64]*store.Node{}
	hosts := map[int64]string{}
	reqs := map[int64]proto.SyncRequest{}
	for _, n := range nodes {
		nm[n.ID] = n
		req := nodeReq(n)
		reqs[n.ID] = req
		hosts[n.ID] = entryHost(n, req.Net)
	}
	pools, _ := s.Store.ListPools(0)
	pm := map[int64]*store.Pool{}
	for _, p := range pools {
		pm[p.ID] = p
	}
	var freeIDs map[int64]bool
	if !s.pro() {
		_, freeIDs = s.freeAllowed()
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	now := time.Now().Unix()
	out := make([]proxyView, 0, len(ps))
	for _, p := range ps {
		v := proxyView{Proxy: p, Host: hosts[p.NodeID]}
		if p.Scope == "local" {
			v.Host = "127.0.0.1"
		}
		if n := nm[p.NodeID]; n != nil {
			v.NodeName = n.Name
			st := reqs[n.ID]
			if p.Scope != "local" {
				v.TSHost = st.Net.TailscaleIP
			}
			if p.Scope == "any" && n.UPnP && st.UPnP != nil && st.UPnP.State == "ok" && st.UPnP.ExternalIP != "" {
				v.WANHost = st.UPnP.ExternalIP
			}
			if rn := nm[p.RelayNodeID]; rn != nil && p.RelayNodeID != p.NodeID {
				v.RelayName, v.RelayHost = rn.Name, relayHost(rn, reqs[rn.ID].Net)
				for _, r := range st.Relays {
					if r.Role == "client" && r.PeerNodeID == rn.ID && r.Connected {
						v.RelayUp = true
					}
				}
			}
		}
		if pl := pm[p.PoolID]; pl != nil {
			v.PoolName = pl.Name
		}
		if ip, err := ProxyIP(p, pm[p.PoolID]); err == nil {
			v.IP = ip.String()
			v.Subnet64 = v6.Slash64(ip).String()
		}
		live := s.live[p.NodeID]
		online := nm[p.NodeID] != nil && now-nm[p.NodeID].LastSeen <= onlineWindow
		switch {
		case freeIDs != nil && !freeIDs[p.ID]:
			v.Status, v.Code = "license", "license_required"
		case !p.Enabled:
			v.Status = "disabled"
		case nm[p.NodeID] != nil && nm[p.NodeID].Paused:
			v.Status = "paused"
		case p.ExpiresAt > 0 && p.ExpiresAt < now:
			v.Status = "expired"
		case !online || live == nil:
			v.Status = "offline"
		default:
			if a, ok := live.Applied[p.ID]; ok {
				if a.OK && a.IP == v.IP {
					v.Status = "ok"
				} else if a.Error != "" {
					v.Status, v.Error, v.Code = "error", a.Error, a.Code
				} else {
					v.Status = "pending"
				}
			} else {
				v.Status = "pending"
			}
			v.Active = live.Active[p.ID]
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) handleListProxies(w http.ResponseWriter, r *http.Request) {
	nid, _ := strconv.ParseInt(r.URL.Query().Get("node_id"), 10, 64)
	f := store.ProxyFilter{NodeID: nid}
	if isCustomer(r) {
		f = store.ProxyFilter{OwnerID: currentUser(r).ID}
	}
	vs, err := s.proxyViews(f)
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, vs)
}

const alnum = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alnum))))
		b[i] = alnum[k.Int64()]
	}
	return string(b)
}

type bulkReq struct {
	NodeID       int64  `json:"node_id"`
	PoolID       int64  `json:"pool_id"`
	Count        int    `json:"count"`
	StartPort    int    `json:"start_port"`
	Protocol     string `json:"protocol"`
	Auth         string `json:"auth"` // none | random | fixed
	User         string `json:"user"`
	Pass         string `json:"pass"`
	Spread       bool   `json:"spread"` // each proxy in its own /64 (pools larger than /64)
	IPv4Fallback bool   `json:"ipv4_fallback"`
	Tag          string `json:"tag"`
	Note         string `json:"note"`
	ExpiresDays  int    `json:"expires_days"`
	LabelPrefix  string `json:"label_prefix"` // "FB" -> "FB 01", "FB 02"…
	Scope        string `json:"scope"`        // local | lan | any
	MaxConns     int    `json:"max_conns"`
	RateKBps     int    `json:"rate_kbps"`
	RelayNodeID  int64  `json:"relay_node_id"`
	Iface        string `json:"iface"` // network card ("" = node default)
}

// checkIface validates a per-proxy network card: it must be connected on
// the node and carry the pool's IPv6 prefix.
func (s *Server) checkIface(nodeID int64, poolID int64, iface string) error {
	if iface == "" {
		return nil
	}
	n, err := s.Store.GetNode(nodeID)
	if err != nil {
		return errors.New("node not found")
	}
	pool, _ := s.Store.GetPool(poolID)
	for _, it := range nodeReq(n).Net.Interfaces {
		if it.Name != iface {
			continue
		}
		if pool == nil || pool.CIDR == "" {
			return nil
		}
		for _, p := range it.Prefixes {
			if p == pool.CIDR {
				return nil
			}
		}
		return fmt.Errorf("network card %s has a different IPv6 prefix than pool %s; create a pool for its prefix first", iface, pool.CIDR)
	}
	return fmt.Errorf("network card %s is not connected on this machine", iface)
}

func validLimits(maxConns, rate int) error {
	if maxConns < 0 || maxConns > 100000 {
		return errors.New("max connections must be 0 (unlimited) to 100000")
	}
	if rate < 0 || rate > 10_000_000 {
		return errors.New("speed limit must be 0 (unlimited) to 10000000 KB/s")
	}
	return nil
}

func (s *Server) reservedPort(p int) bool {
	return p == s.ListenPort || p < 1024 || p > 65535
}

func (s *Server) handleBulkCreate(w http.ResponseWriter, r *http.Request) {
	var b bulkReq
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.Count < 1 || b.Count > 5000 {
		fail(w, 400, "count must be 1..5000")
		return
	}
	pool, err := s.Store.GetPool(b.PoolID)
	if err != nil || pool.NodeID != b.NodeID {
		fail(w, 400, "pool not found on this node")
		return
	}
	if b.Protocol == "" {
		b.Protocol = "both"
	}
	if b.Protocol != "both" && b.Protocol != "http" && b.Protocol != "socks5" {
		fail(w, 400, "protocol must be both, http or socks5")
		return
	}
	if b.Scope == "" {
		b.Scope = "lan"
	}
	if err := validScope(b.Scope, b.Auth != "none"); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.Auth == "fixed" && (b.User == "" || b.Pass == "") {
		fail(w, 400, "user and password required")
		return
	}
	if err := validLimits(b.MaxConns, b.RateKBps); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !s.pro() {
		all, _ := s.Store.ListProxies(store.ProxyFilter{})
		if len(all)+b.Count > license.FreeProxies && s.needPro(w, "More than "+itoa(license.FreeProxies)+" proxies") {
			return
		}
		if (b.MaxConns > 0 || b.RateKBps > 0) && s.needPro(w, "Per-proxy limits") {
			return
		}
		if b.RelayNodeID != 0 && s.needPro(w, "Sharing through a relay") {
			return
		}
		if b.Iface != "" && s.needPro(w, "Network card per proxy") {
			return
		}
	}
	if err := s.checkIface(b.NodeID, b.PoolID, strings.TrimSpace(b.Iface)); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.RelayNodeID != 0 && b.Auth == "none" {
		fail(w, 400, "proxies shared through a relay must have a username and password")
		return
	}
	var relayUsed map[int]bool
	if b.RelayNodeID != 0 {
		var err error
		if relayUsed, err = s.usedPorts(b.RelayNodeID, 0); err != nil {
			logErr(w, err)
			return
		}
	}
	if b.StartPort == 0 {
		b.StartPort = 10001
	}
	used, err := s.usedPorts(b.NodeID, 0)
	if err != nil {
		logErr(w, err)
		return
	}
	var subnetCount uint64 = 1
	if pool.CIDR != "" {
		subnetCount = v6.SubnetCount(netip.MustParsePrefix(pool.CIDR))
	} else if !pool.Auto {
		fail(w, 400, "pool has no prefix")
		return
	}
	// subnets already taken in this pool, to spread new proxies over unused /64s
	takenSubnets := map[uint64]bool{}
	if b.Spread && subnetCount > 1 {
		existing, _ := s.Store.ListProxies(store.ProxyFilter{NodeID: b.NodeID})
		for _, p := range existing {
			if p.PoolID == pool.ID {
				takenSubnets[p.Subnet] = true
			}
		}
		if uint64(b.Count)+uint64(len(takenSubnets)) > subnetCount {
			fail(w, 400, fmt.Sprintf("pool only has %d /64 subnets (%d already used)", subnetCount, len(takenSubnets)))
			return
		}
	}
	var expires int64
	if b.ExpiresDays > 0 {
		expires = time.Now().Add(time.Duration(b.ExpiresDays) * 24 * time.Hour).Unix()
	}
	port := b.StartPort
	var ps []*store.Proxy
	for i := 0; i < b.Count; i++ {
		for used[port] || s.reservedPort(port) {
			port++
			if port > 65535 {
				fail(w, 400, "no free ports left")
				return
			}
		}
		used[port] = true
		p := &store.Proxy{NodeID: b.NodeID, PoolID: pool.ID, Port: port, Protocol: b.Protocol, Enabled: true,
			Suffix: v6.FormatSuffix(v6.RandomSuffix()), IPv4Fallback: b.IPv4Fallback, Tag: b.Tag, Note: b.Note, ExpiresAt: expires,
			Scope: b.Scope, MaxConns: b.MaxConns, RateKBps: b.RateKBps, Iface: strings.TrimSpace(b.Iface)}
		if b.Spread && subnetCount > 1 {
			for {
				sn := v6.RandomSubnet(subnetCount)
				if !takenSubnets[sn] {
					takenSubnets[sn] = true
					p.Subnet = sn
					break
				}
			}
		}
		if lp := strings.TrimSpace(b.LabelPrefix); lp != "" {
			p.Label = fmt.Sprintf("%s %0*d", lp, len(strconv.Itoa(b.Count)), i+1)
		}
		switch b.Auth {
		case "random":
			p.AuthUser, p.AuthPass = "u"+randString(7), randString(12)
		case "fixed":
			p.AuthUser, p.AuthPass = b.User, b.Pass
		}
		if b.RelayNodeID != 0 {
			if err := s.setRelay(p, b.RelayNodeID, 0, relayUsed); err != nil {
				fail(w, 400, err.Error())
				return
			}
		}
		ps = append(ps, p)
	}
	if err := s.Store.CreateProxies(ps); err != nil {
		logErr(w, err)
		return
	}
	s.Store.AddEvent(b.NodeID, "info", fmt.Sprintf("%d proxies created (ports %d-%d)", len(ps), ps[0].Port, ps[len(ps)-1].Port))
	writeJSON(w, 200, map[string]any{"created": len(ps)})
}

func (s *Server) handleUpdateProxy(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.GetProxy(pathID(r))
	if err != nil {
		fail(w, 404, "proxy not found")
		return
	}
	var b struct {
		Port         *int    `json:"port"`
		Protocol     *string `json:"protocol"`
		AuthUser     *string `json:"auth_user"`
		AuthPass     *string `json:"auth_pass"`
		IPv4Fallback *bool   `json:"ipv4_fallback"`
		Enabled      *bool   `json:"enabled"`
		Note         *string `json:"note"`
		Label        *string `json:"label"`
		Scope        *string `json:"scope"`
		Tag          *string `json:"tag"`
		ExpiresAt    *int64  `json:"expires_at"`
		IP           *string `json:"ip"` // set a specific address inside the pool
		MaxConns     *int    `json:"max_conns"`
		RateKBps     *int    `json:"rate_kbps"`
		RelayNodeID  *int64  `json:"relay_node_id"`
		RelayPort    *int    `json:"relay_port"`
		Iface        *string `json:"iface"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.Port != nil && *b.Port != p.Port {
		if s.reservedPort(*b.Port) {
			fail(w, 400, "port not allowed")
			return
		}
		used, _ := s.usedPorts(p.NodeID, p.ID)
		if used[*b.Port] {
			fail(w, 400, "port already used on this node")
			return
		}
		p.Port = *b.Port
	}
	if b.Protocol != nil {
		if *b.Protocol != "both" && *b.Protocol != "http" && *b.Protocol != "socks5" {
			fail(w, 400, "bad protocol")
			return
		}
		p.Protocol = *b.Protocol
	}
	if b.AuthUser != nil {
		p.AuthUser = strings.TrimSpace(*b.AuthUser)
	}
	if b.AuthPass != nil {
		p.AuthPass = *b.AuthPass
	}
	if b.Scope != nil {
		p.Scope = *b.Scope
	}
	if err := validScope(p.Scope, p.AuthUser != ""); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if (p.AuthUser == "") != (p.AuthPass == "") {
		fail(w, 400, "set both user and password, or neither")
		return
	}
	if strings.ContainsAny(p.AuthUser+p.AuthPass, ":@/ ") {
		fail(w, 400, "user/password must not contain : @ / or spaces")
		return
	}
	if b.IPv4Fallback != nil {
		p.IPv4Fallback = *b.IPv4Fallback
	}
	if b.Enabled != nil {
		p.Enabled = *b.Enabled
	}
	if b.Note != nil {
		p.Note = *b.Note
	}
	if b.Label != nil {
		p.Label = strings.TrimSpace(*b.Label)
	}
	if b.Tag != nil {
		p.Tag = strings.TrimSpace(*b.Tag)
	}
	if b.ExpiresAt != nil {
		p.ExpiresAt = *b.ExpiresAt
	}
	if ((b.MaxConns != nil && *b.MaxConns > 0) || (b.RateKBps != nil && *b.RateKBps > 0)) && s.needPro(w, "Per-proxy limits") {
		return
	}
	if b.RelayNodeID != nil && *b.RelayNodeID != 0 && s.needPro(w, "Sharing through a relay") {
		return
	}
	if b.Iface != nil && strings.TrimSpace(*b.Iface) != p.Iface {
		v := strings.TrimSpace(*b.Iface)
		if v != "" && s.needPro(w, "Network card per proxy") {
			return
		}
		if err := s.checkIface(p.NodeID, p.PoolID, v); err != nil {
			fail(w, 400, err.Error())
			return
		}
		p.Iface = v
	}
	if b.MaxConns != nil {
		p.MaxConns = *b.MaxConns
	}
	if b.RateKBps != nil {
		p.RateKBps = *b.RateKBps
	}
	if err := validLimits(p.MaxConns, p.RateKBps); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if b.RelayNodeID != nil || b.RelayPort != nil || p.RelayNodeID != 0 {
		rid, pub := p.RelayNodeID, 0
		if b.RelayNodeID != nil {
			rid = *b.RelayNodeID
		}
		if b.RelayPort != nil {
			pub = *b.RelayPort
		}
		if err := s.setRelay(p, rid, pub, nil); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	if b.IP != nil && strings.TrimSpace(*b.IP) != "" {
		pool, err := s.Store.GetPool(p.PoolID)
		if err != nil || pool.CIDR == "" {
			fail(w, 400, "pool has no prefix yet")
			return
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(*b.IP))
		pfx := netip.MustParsePrefix(pool.CIDR)
		if err != nil || !pfx.Contains(ip) {
			fail(w, 400, "IP must be inside "+pool.CIDR)
			return
		}
		a := ip.As16()
		hi := uint64(0)
		for i := 0; i < 8; i++ {
			hi = hi<<8 | uint64(a[i])
		}
		lo := uint64(0)
		for i := 8; i < 16; i++ {
			lo = lo<<8 | uint64(a[i])
		}
		p.Subnet = hi & (v6.SubnetCount(pfx) - 1)
		if v6.SubnetCount(pfx) <= 1 {
			p.Subnet = 0
		}
		p.Suffix = v6.FormatSuffix(lo)
	}
	if err := s.Store.UpdateProxy(p); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleProxyAction(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
		Value  string  `json:"value"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(b.IDs) == 0 {
		fail(w, 400, "no proxies selected")
		return
	}
	ps, err := s.Store.ListProxies(store.ProxyFilter{IDs: b.IDs})
	if err != nil {
		logErr(w, err)
		return
	}
	if isCustomer(r) {
		me := currentUser(r).ID
		for _, p := range ps {
			if p.OwnerID != me {
				fail(w, 403, "not your proxy")
				return
			}
		}
		if b.Action != "test" && b.Action != "regen_pass" && b.Action != "regen_ip" {
			fail(w, 403, "not available for customer accounts")
			return
		}
	}
	if b.Action == "set_owner" {
		oid, _ := strconv.ParseInt(b.Value, 10, 64)
		if oid != 0 {
			if s.needPro(w, "Customer accounts") {
				return
			}
			if u, err := s.Store.GetUser(oid); err != nil || u.Role != "customer" {
				fail(w, 400, "customer account not found")
				return
			}
		}
		for _, p := range ps {
			p.OwnerID = oid
			if err := s.Store.UpdateProxy(p); err != nil {
				logErr(w, err)
				return
			}
		}
		writeJSON(w, 200, map[string]int{"affected": len(ps)})
		return
	}
	switch b.Action {
	case "delete":
		if err := s.Store.DeleteProxies(b.IDs); err != nil {
			logErr(w, err)
			return
		}
		s.Store.AddEvent(0, "info", fmt.Sprintf("%d proxies deleted", len(b.IDs)))
		writeJSON(w, 200, map[string]int{"affected": len(b.IDs)})
		return
	case "test":
		var cmds []*commandState
		for _, p := range ps {
			cmds = append(cmds, s.queueCommand(p.NodeID, proto.Command{Type: "test_proxy", ProxyID: p.ID}))
		}
		writeJSON(w, 200, map[string]any{"commands": cmds})
		return
	case "reset_stats":
		if err := s.Store.ResetStats(b.IDs); err != nil {
			logErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]int{"affected": len(b.IDs)})
		return
	}
	pools, _ := s.Store.ListPools(0)
	pm := map[int64]*store.Pool{}
	for _, p := range pools {
		pm[p.ID] = p
	}
	if (b.Action == "set_limits" && b.Value != "0,0") || (b.Action == "set_relay" && b.Value != "0") {
		if s.needPro(w, "This action") {
			return
		}
	}
	var relayUsed map[int]bool
	usedBy := map[int64]map[int]bool{}
	for _, p := range ps {
		switch b.Action {
		case "fix_port": // the port is taken by another program: move to a free one
			used := usedBy[p.NodeID]
			if used == nil {
				var err error
				if used, err = s.usedPorts(p.NodeID, 0); err != nil {
					logErr(w, err)
					return
				}
				usedBy[p.NodeID] = used
			}
			np := p.Port + 1
			for used[np] || s.reservedPort(np) {
				np++
				if np > 65535 {
					fail(w, 400, "no free ports left")
					return
				}
			}
			used[np] = true
			p.Port = np
		case "enable":
			p.Enabled = true
		case "disable":
			p.Enabled = false
		case "regen_ip":
			p.Suffix = v6.FormatSuffix(v6.RandomSuffix())
			if pl := pm[p.PoolID]; pl != nil && pl.CIDR != "" {
				if c := v6.SubnetCount(netip.MustParsePrefix(pl.CIDR)); c > 1 {
					p.Subnet = v6.RandomSubnet(c)
				}
			}
		case "regen_pass":
			if p.AuthUser == "" {
				p.AuthUser = "u" + randString(7)
			}
			p.AuthPass = randString(12)
		case "remove_auth":
			if p.RelayNodeID != 0 {
				fail(w, 400, fmt.Sprintf("proxy :%d is shared through a relay and must keep a password", p.Port))
				return
			}
			if p.Scope == "any" {
				fail(w, 400, fmt.Sprintf("proxy :%d is shared to the Internet and must keep a password; change its sharing scope first", p.Port))
				return
			}
			p.AuthUser, p.AuthPass = "", ""
		case "set_scope":
			if err := validScope(b.Value, p.AuthUser != ""); err != nil {
				fail(w, 400, fmt.Sprintf(":%d: %v", p.Port, err))
				return
			}
			p.Scope = b.Value
		case "set_tag":
			p.Tag = strings.TrimSpace(b.Value)
		case "set_limits": // "maxConns,rateKBps"
			var mc, rk int
			if _, err := fmt.Sscanf(b.Value, "%d,%d", &mc, &rk); err != nil {
				fail(w, 400, "limits must be \"max_conns,rate_kbps\"")
				return
			}
			if err := validLimits(mc, rk); err != nil {
				fail(w, 400, err.Error())
				return
			}
			p.MaxConns, p.RateKBps = mc, rk
		case "set_relay":
			rid, _ := strconv.ParseInt(b.Value, 10, 64)
			if relayUsed == nil && rid != 0 {
				var err error
				if relayUsed, err = s.usedPorts(rid, 0); err != nil {
					logErr(w, err)
					return
				}
			}
			if p.RelayNodeID == rid {
				continue
			}
			if err := s.setRelay(p, rid, 0, relayUsed); err != nil {
				fail(w, 400, err.Error())
				return
			}
		case "set_protocol":
			if b.Value != "both" && b.Value != "http" && b.Value != "socks5" {
				fail(w, 400, "bad protocol")
				return
			}
			p.Protocol = b.Value
		case "fallback_on":
			p.IPv4Fallback = true
		case "fallback_off":
			p.IPv4Fallback = false
		default:
			fail(w, 400, "unknown action")
			return
		}
		if err := s.Store.UpdateProxy(p); err != nil {
			logErr(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]int{"affected": len(ps)})
}

func validScope(scope string, hasAuth bool) error {
	switch scope {
	case "local", "lan":
		return nil
	case "any":
		if !hasAuth {
			return errors.New("a proxy shared to the Internet must have a username and password (otherwise anyone could use it)")
		}
		return nil
	}
	return errors.New("scope must be local, lan or any")
}

// ---- export ----

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs    []int64 `json:"ids"`
		Format string  `json:"format"`
		Scheme string  `json:"scheme"` // http | socks5 (for URL formats)
		Host   string  `json:"host"`   // override entry host
		Via    string  `json:"via"`    // "" (LAN/entry) | relay | tailscale | wan
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	pf := store.ProxyFilter{IDs: b.IDs}
	if isCustomer(r) {
		pf.OwnerID = currentUser(r).ID
	}
	vs, err := s.proxyViews(pf)
	if err != nil {
		logErr(w, err)
		return
	}
	if len(b.IDs) == 0 {
		vs = nil
	}
	vs, skipped := viaEndpoint(vs, b.Via)
	text, err := exportProxies(vs, b.Format, b.Scheme, strings.TrimSpace(b.Host))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"text": text, "skipped": skipped})
}

// viaEndpoint rewrites host/port for the chosen way of reaching the proxies
// and drops proxies that cannot be reached that way.
func viaEndpoint(vs []proxyView, via string) ([]proxyView, int) {
	if via == "" || via == "lan" {
		return vs, 0
	}
	out := make([]proxyView, 0, len(vs))
	for _, v := range vs {
		cp := *v.Proxy
		v.Proxy = &cp
		switch via {
		case "relay":
			if v.RelayHost == "" {
				continue
			}
			v.Host, v.Port = v.RelayHost, relayPub(&cp)
		case "tailscale":
			if v.TSHost == "" {
				continue
			}
			v.Host = v.TSHost
		case "wan":
			if v.WANHost == "" {
				continue
			}
			v.Host = v.WANHost
		default:
			continue
		}
		out = append(out, v)
	}
	return out, len(vs) - len(out)
}

func exportProxies(vs []proxyView, format, scheme, hostOverride string) (string, error) {
	if scheme == "" {
		scheme = "http"
	}
	var sb strings.Builder
	switch format {
	case "csv":
		cw := csv.NewWriter(&sb)
		_ = cw.Write([]string{"name", "host", "port", "user", "pass", "protocol", "ipv6", "tag", "note"})
		for _, v := range vs {
			_ = cw.Write([]string{v.Label, hostFor(v, hostOverride), strconv.Itoa(v.Port), v.AuthUser, v.AuthPass, v.Protocol, v.IP, v.Tag, v.Note})
		}
		cw.Flush()
		return sb.String(), nil
	case "json":
		type item struct {
			Name     string `json:"name,omitempty"`
			Host     string `json:"host"`
			Port     int    `json:"port"`
			User     string `json:"user,omitempty"`
			Pass     string `json:"pass,omitempty"`
			Protocol string `json:"protocol"`
			IPv6     string `json:"ipv6"`
			Tag      string `json:"tag,omitempty"`
			Note     string `json:"note,omitempty"`
		}
		items := []item{}
		for _, v := range vs {
			items = append(items, item{v.Label, hostFor(v, hostOverride), v.Port, v.AuthUser, v.AuthPass, v.Protocol, v.IP, v.Tag, v.Note})
		}
		bs, _ := json.MarshalIndent(items, "", "  ")
		return string(bs), nil
	}
	for _, v := range vs {
		h := hostFor(v, hostOverride)
		hp := h + ":" + strconv.Itoa(v.Port)
		if strings.Contains(h, ":") {
			hp = "[" + h + "]:" + strconv.Itoa(v.Port)
		}
		auth := v.AuthUser != ""
		var line string
		switch format {
		case "", "host:port:user:pass":
			line = hp
			if auth {
				line += ":" + v.AuthUser + ":" + v.AuthPass
			}
		case "user:pass@host:port":
			line = hp
			if auth {
				line = v.AuthUser + ":" + v.AuthPass + "@" + hp
			}
		case "url":
			line = scheme + "://"
			if auth {
				line += v.AuthUser + ":" + v.AuthPass + "@"
			}
			line += hp
		case "scheme://host:port:user:pass":
			line = scheme + "://" + hp
			if auth {
				line += ":" + v.AuthUser + ":" + v.AuthPass
			}
		case "host:port@user:pass":
			line = hp
			if auth {
				line += "@" + v.AuthUser + ":" + v.AuthPass
			}
		default:
			return "", errors.New("unknown format")
		}
		sb.WriteString(line + "\n")
	}
	return sb.String(), nil
}

func hostFor(v proxyView, override string) string {
	if override != "" {
		return override
	}
	return v.Host
}

// ---- dashboard, events, settings ----

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	vs, err := s.proxyViews(store.ProxyFilter{})
	if err != nil {
		logErr(w, err)
		return
	}
	nodes, _ := s.Store.ListNodes()
	online := 0
	for _, n := range nodes {
		if time.Now().Unix()-n.LastSeen <= onlineWindow {
			online++
		}
	}
	status := map[string]int{}
	var up, down, active int64
	subnets := map[string]bool{}
	failing := 0
	for _, v := range vs {
		status[v.Status]++
		if v.Enabled && v.CheckOK == 0 && v.FailStreak >= 2 {
			failing++
		}
		up += v.BytesUp
		down += v.BytesDown
		active += v.Active
		if v.Subnet64 != "" {
			subnets[v.Subnet64] = true
		}
	}
	events, _ := s.Store.ListEvents(15)
	writeJSON(w, 200, map[string]any{
		"nodes": len(nodes), "nodes_online": online, "proxies": len(vs), "status": status,
		"bytes_up": up, "bytes_down": down, "active": active, "subnets": len(subnets), "events": events,
		"version": proto.Version, "failing": failing, "pro": s.pro(),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ev, err := s.Store.ListEvents(200)
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, ev)
}

var settingKeys = []string{"panel_url", "default_scheme", "check_interval_min", "auto_firewall", "telegram_chat",
	"notify_node_offline", "notify_proxy_fail", "notify_prefix_change", "notify_update", "lang", "instance_name"}

var settingDefaults = map[string]string{"default_scheme": "http", "check_interval_min": "10", "auto_firewall": "1",
	"notify_node_offline": "1", "notify_proxy_fail": "1", "notify_prefix_change": "1", "notify_update": "1",
	"lang": "vi"}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"detected_url": s.panelURL(r)}
	for _, k := range settingKeys {
		out[k] = s.Store.GetSetting(k, settingDefaults[k])
	}
	// never send the bot token back to the browser
	out["telegram_token_set"] = s.Store.GetSetting("telegram_token", "") != ""
	writeJSON(w, 200, out)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var b map[string]string
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if v, ok := b["check_interval_min"]; ok {
		if n, err := strconv.Atoi(v); err != nil || n < 0 || n > 1440 {
			fail(w, 400, "check interval must be 0..1440 minutes")
			return
		}
	}
	for _, k := range settingKeys {
		if v, ok := b[k]; ok {
			_ = s.Store.SetSetting(k, strings.TrimSpace(v))
		}
	}
	if v, ok := b["telegram_token"]; ok && strings.TrimSpace(v) != "" && s.needPro(w, "Telegram alerts") {
		return
	}
	if v, ok := b["telegram_token"]; ok && (strings.TrimSpace(v) != "" || b["telegram_token_clear"] == "1") {
		_ = s.Store.SetSetting("telegram_token", strings.TrimSpace(v))
	}
	s.handleGetSettings(w, r)
}
