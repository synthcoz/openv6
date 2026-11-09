// Package proto defines the JSON messages exchanged between panel and agents.
// Agents always initiate the connection (polling), so a node behind NAT/CGNAT
// works as long as it can reach the panel.
package proto

const Version = "0.2.0"

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
	Logs         []string             `json:"logs"`   // new log lines since the last successful sync
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
	DefaultIface string      `json:"default_iface"` // iface of the IPv6 default route
	HasV6Route   bool        `json:"has_v6_route"`
	PublicV4     string      `json:"public_v4,omitempty"`
	LANv4        []string    `json:"lan_v4"`
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
}

type CommandResult struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Output  string `json:"output"`
	IP      string `json:"ip,omitempty"`
	Latency int    `json:"latency,omitempty"` // ms
	Country string `json:"country,omitempty"`
	Org     string `json:"org,omitempty"`
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
}

type Command struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // test_proxy | test_v6 | update
	ProxyID int64  `json:"proxy_id,omitempty"`
	Version string `json:"version,omitempty"` // update: expected version
	URL     string `json:"url,omitempty"`     // update: binary download URL
}
