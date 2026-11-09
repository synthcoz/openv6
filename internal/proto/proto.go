// Package proto defines the JSON messages exchanged between panel and agents.
// Agents always initiate the connection (polling), so a node behind NAT/CGNAT
// works as long as it can reach the panel.
package proto

const Version = "0.3.0"

// SyncRequest is sent by an agent every few seconds.
type SyncRequest struct {
	AgentVersion string               `json:"agent_version"`
	Hostname     string               `json:"hostname"`
	OS           string               `json:"os"`
	Net          NetState             `json:"net"`
	Stats        map[int64]ProxyStats `json:"stats"`   // deltas since last successful sync
	Applied      map[int64]Applied    `json:"applied"` // current status per proxy
	Results      []CommandResult      `json:"results"`
	Errors       []string             `json:"errors"`
	Checks       []SystemCheck        `json:"checks"` // preflight / health of the machine
	StartedAt    int64                `json:"started_at"`
	Logs         []string             `json:"logs"` // new log lines since the last successful sync
	Arch         string               `json:"arch"`
}

// SystemCheck is one line of the node's self-diagnosis shown in the panel.
type SystemCheck struct {
	ID     string `json:"id"`     // stable key, translated by the UI
	OK     bool   `json:"ok"`     // false = problem
	Warn   bool   `json:"warn"`   // true = not fatal
	Detail string `json:"detail"` // value or error text
}

type NetState struct {
	Interfaces   []Interface `json:"interfaces"`
	DefaultIface string      `json:"default_iface"` // iface of the IPv6 default route (main table)
	HasV6Route   bool        `json:"has_v6_route"`
	// PrimaryIface is the physical interface whose /64 the proxies use: it
	// has a global (non-ULA) prefix and is not a VPN tunnel.
	PrimaryIface string `json:"primary_iface"`
	// EgressIface is the interface the kernel picks for an Internet IPv6
	// destination right now; a VPN that captures traffic shows up here.
	EgressIface  string   `json:"egress_iface"`
	Gateway      string   `json:"gateway,omitempty"` // IPv6 router of PrimaryIface
	VPNs         []VPN    `json:"vpns"`
	IPv6Disabled bool     `json:"ipv6_disabled"`
	Bypass       string   `json:"bypass"` // off | ok | error: VPN bypass routing status
	BypassError  string   `json:"bypass_error,omitempty"`
	PublicV4     string   `json:"public_v4,omitempty"`
	LANv4        []string `json:"lan_v4"`
}

// VPN is a tunnel interface detected on the node.
type VPN struct {
	Iface string `json:"iface"`
	Kind  string `json:"kind"` // warp | tailscale | wireguard | nordvpn | protonvpn | openvpn | zerotier | ppp | other
	Up    bool   `json:"up"`
}

type Interface struct {
	Name     string   `json:"name"`
	Prefixes []string `json:"prefixes"` // global /64 (or shorter) prefixes learned from SLAAC/static addrs
	Addrs    []string `json:"addrs"`    // global IPv6 addresses (excluding ones the agent added)
}

type ProxyStats struct {
	Up     int64 `json:"up"`
	Down   int64 `json:"down"`
	Conns  int64 `json:"conns"`
	Active int64 `json:"active"`
}

type Applied struct {
	IP    string `json:"ip"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"` // machine-readable error code, translated by the UI
}

type CommandResult struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Output  string `json:"output"`
	IP      string `json:"ip,omitempty"`
	Latency int    `json:"latency,omitempty"` // ms
	Country string `json:"country,omitempty"`
	Org     string `json:"org,omitempty"`
	Code    string `json:"code,omitempty"` // error code for failed tests
}

// SyncResponse carries the desired state for the node.
type SyncResponse struct {
	NodeID     int64       `json:"node_id"`
	NodeName   string      `json:"node_name"`
	Iface      string      `json:"iface"` // empty = use default iface
	AllowCIDRs []string    `json:"allow_cidrs"`
	Pools      []PoolSpec  `json:"pools"`
	Proxies    []ProxySpec `json:"proxies"`
	Commands   []Command   `json:"commands"`
	Interval   int         `json:"interval"` // seconds until next sync
	Firewall   bool        `json:"firewall"` // manage ufw rules for proxy ports
	Bypass     bool        `json:"bypass"`   // keep proxy traffic on the physical NIC when a VPN is active
	DNS        []string    `json:"dns"`      // resolvers used by proxies; empty = system resolver
}

type PoolSpec struct {
	ID   int64  `json:"id"`
	CIDR string `json:"cidr"`
	Mode string `json:"mode"` // addr | anyip
	Auto bool   `json:"auto"`
}

type ProxySpec struct {
	ID           int64  `json:"id"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol"`
	User         string `json:"user"`
	Pass         string `json:"pass"`
	IP           string `json:"ip"`
	PoolID       int64  `json:"pool_id"`
	IPv4Fallback bool   `json:"ipv4_fallback"`
	Scope        string `json:"scope"` // local | lan | any
}

type Command struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // test_proxy | test_v6 | update
	ProxyID int64  `json:"proxy_id,omitempty"`
	Version string `json:"version,omitempty"` // update: expected version
	URL     string `json:"url,omitempty"`     // update: binary download URL
}
