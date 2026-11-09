package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// ---- users & sessions ----

var (
	dummyOnce sync.Once
	dummyHash []byte
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Store) UserCount() int {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n
}

func (s *Store) CreateUser(username, password, role string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO users(username,pass_hash,role,created_at) VALUES(?,?,?,?)`, username, string(h), role, Now())
	return err
}

func (s *Store) SetPassword(username, password string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(`UPDATE users SET pass_hash=? WHERE username=?`, string(h), username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.DB.Exec(`DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE username=?)`, username)
	return err
}

func (s *Store) CheckLogin(username, password string) (*User, error) {
	var u User
	var hash string
	err := s.DB.QueryRow(`SELECT id,username,role,pass_hash FROM users WHERE username=?`, username).Scan(&u.ID, &u.Username, &u.Role, &hash)
	if err != nil {
		// burn similar time to avoid user enumeration
		dummyOnce.Do(func() { dummyHash, _ = bcrypt.GenerateFromPassword([]byte("x"), bcrypt.DefaultCost) })
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrNotFound
	}
	return &u, nil
}

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

func (s *Store) CreateSession(userID int64, ttl int64) (string, error) {
	tok := RandomToken(32)
	_, err := s.DB.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, hashToken(tok), userID, Now()+ttl)
	_, _ = s.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, Now())
	return tok, err
}

func (s *Store) SessionUser(tok string) (*User, error) {
	var u User
	err := s.DB.QueryRow(`SELECT u.id,u.username,u.role FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, hashToken(tok), Now()).Scan(&u.ID, &u.Username, &u.Role)
	if err != nil {
		return nil, ErrNotFound
	}
	return &u, nil
}

func (s *Store) DeleteSession(tok string) {
	_, _ = s.DB.Exec(`DELETE FROM sessions WHERE token_hash=?`, hashToken(tok))
}

// ---- nodes ----

type Node struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Token      string `json:"token,omitempty"`
	EntryHost  string `json:"entry_host"`
	Iface      string `json:"iface"`
	AllowCIDRs string `json:"allow_cidrs"`
	IsLocal    bool   `json:"is_local"`
	Bypass     bool   `json:"bypass"`
	DNS        string `json:"dns"` // "" system | comma separated resolver IPs
	LastSeen   int64  `json:"last_seen"`
	StateJSON  string `json:"-"`
	CreatedAt  int64  `json:"created_at"`
}

const DefaultAllowCIDRs = "127.0.0.0/8\n10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16\n100.64.0.0/10\n::1/128\nfc00::/7\nfe80::/10"

const nodeCols = `id,name,token,entry_host,iface,allow_cidrs,is_local,last_seen,state_json,created_at,bypass,dns`

func scanNode(r interface{ Scan(...any) error }) (*Node, error) {
	var n Node
	var local, bypass int
	if err := r.Scan(&n.ID, &n.Name, &n.Token, &n.EntryHost, &n.Iface, &n.AllowCIDRs, &local, &n.LastSeen, &n.StateJSON, &n.CreatedAt, &bypass, &n.DNS); err != nil {
		return nil, err
	}
	n.IsLocal, n.Bypass = local == 1, bypass == 1
	return &n, nil
}

func (s *Store) CreateNode(name, entryHost string, local bool) (*Node, error) {
	tok := RandomToken(24)
	l := 0
	if local {
		l = 1
	}
	res, err := s.DB.Exec(`INSERT INTO nodes(name,token,entry_host,allow_cidrs,is_local,created_at) VALUES(?,?,?,?,?,?)`,
		name, tok, entryHost, DefaultAllowCIDRs, l, Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	// every node gets an automatic pool that follows the interface's /64
	if _, err := s.DB.Exec(`INSERT INTO pools(node_id,name,auto,mode,created_at) VALUES(?,?,1,'addr',?)`, id, "auto /64", Now()); err != nil {
		return nil, err
	}
	return s.GetNode(id)
}

func (s *Store) GetNode(id int64) (*Node, error) {
	n, err := scanNode(s.DB.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) NodeByToken(tok string) (*Node, error) {
	if tok == "" {
		return nil, ErrNotFound
	}
	n, err := scanNode(s.DB.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE token=?`, tok))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) LocalNode() (*Node, error) {
	n, err := scanNode(s.DB.QueryRow(`SELECT ` + nodeCols + ` FROM nodes WHERE is_local=1 LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) ListNodes() ([]*Node, error) {
	rows, err := s.DB.Query(`SELECT ` + nodeCols + ` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) UpdateNode(n *Node) error {
	_, err := s.DB.Exec(`UPDATE nodes SET name=?,entry_host=?,iface=?,allow_cidrs=?,bypass=?,dns=? WHERE id=?`, n.Name, n.EntryHost, n.Iface, n.AllowCIDRs, b2i(n.Bypass), n.DNS, n.ID)
	return err
}

func (s *Store) RotateNodeToken(id int64) (string, error) {
	tok := RandomToken(24)
	_, err := s.DB.Exec(`UPDATE nodes SET token=? WHERE id=?`, tok, id)
	return tok, err
}

func (s *Store) TouchNode(id int64, stateJSON string) error {
	_, err := s.DB.Exec(`UPDATE nodes SET last_seen=?,state_json=? WHERE id=?`, Now(), stateJSON, id)
	return err
}

func (s *Store) DeleteNode(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM nodes WHERE id=?`, id)
	return err
}

// ---- pools ----

type Pool struct {
	ID        int64  `json:"id"`
	NodeID    int64  `json:"node_id"`
	Name      string `json:"name"`
	Auto      bool   `json:"auto"`
	CIDR      string `json:"cidr"`
	Mode      string `json:"mode"` // addr | anyip
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) ListPools(nodeID int64) ([]*Pool, error) {
	q := `SELECT id,node_id,name,auto,cidr,mode,created_at FROM pools`
	args := []any{}
	if nodeID > 0 {
		q += ` WHERE node_id=?`
		args = append(args, nodeID)
	}
	rows, err := s.DB.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Pool{}
	for rows.Next() {
		var p Pool
		var auto int
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Name, &auto, &p.CIDR, &p.Mode, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Auto = auto == 1
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) GetPool(id int64) (*Pool, error) {
	var p Pool
	var auto int
	err := s.DB.QueryRow(`SELECT id,node_id,name,auto,cidr,mode,created_at FROM pools WHERE id=?`, id).
		Scan(&p.ID, &p.NodeID, &p.Name, &auto, &p.CIDR, &p.Mode, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	p.Auto = auto == 1
	return &p, err
}

func (s *Store) CreatePool(p *Pool) error {
	p.CreatedAt = Now()
	res, err := s.DB.Exec(`INSERT INTO pools(node_id,name,auto,cidr,mode,created_at) VALUES(?,?,0,?,?,?)`, p.NodeID, p.Name, p.CIDR, p.Mode, p.CreatedAt)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdatePool(p *Pool) error {
	_, err := s.DB.Exec(`UPDATE pools SET name=?,cidr=?,mode=? WHERE id=?`, p.Name, p.CIDR, p.Mode, p.ID)
	return err
}

func (s *Store) SetPoolCIDR(id int64, cidr string) error {
	_, err := s.DB.Exec(`UPDATE pools SET cidr=? WHERE id=?`, cidr, id)
	return err
}

func (s *Store) DeletePool(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM pools WHERE id=? AND auto=0`, id)
	return err
}

// ---- proxies ----

type Proxy struct {
	ID           int64  `json:"id"`
	NodeID       int64  `json:"node_id"`
	PoolID       int64  `json:"pool_id"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol"` // both | http | socks5
	AuthUser     string `json:"auth_user"`
	AuthPass     string `json:"auth_pass"`
	Subnet       uint64 `json:"subnet"`
	Suffix       string `json:"suffix"`
	IPv4Fallback bool   `json:"ipv4_fallback"`
	Enabled      bool   `json:"enabled"`
	Note         string `json:"note"`
	Tag          string `json:"tag"`
	ExpiresAt    int64  `json:"expires_at"`
	CreatedAt    int64  `json:"created_at"`
	BytesUp      int64  `json:"bytes_up"`
	BytesDown    int64  `json:"bytes_down"`
	ConnsTotal   int64  `json:"conns_total"`
	CheckAt      int64  `json:"check_at"`
	CheckResult  string `json:"check_result"`
	CheckOK      int    `json:"check_ok"` // -1 never checked, 0 failed, 1 ok
	CheckLatency int    `json:"check_latency"`
	CheckCountry string `json:"check_country"`
	CheckOrg     string `json:"check_org"`
	FailStreak   int    `json:"fail_streak"`
	Label        string `json:"label"`
	Scope        string `json:"scope"` // local | lan | any
	CheckCode    string `json:"check_code"`
}

const proxyCols = `id,node_id,pool_id,port,protocol,auth_user,auth_pass,subnet,suffix,ipv4_fallback,enabled,note,tag,expires_at,created_at,bytes_up,bytes_down,conns_total,check_at,check_result,check_ok,check_latency,check_country,check_org,fail_streak,label,scope,check_code`

func scanProxy(r interface{ Scan(...any) error }) (*Proxy, error) {
	var p Proxy
	var fb, en int
	var subnet int64
	if err := r.Scan(&p.ID, &p.NodeID, &p.PoolID, &p.Port, &p.Protocol, &p.AuthUser, &p.AuthPass, &subnet, &p.Suffix, &fb, &en,
		&p.Note, &p.Tag, &p.ExpiresAt, &p.CreatedAt, &p.BytesUp, &p.BytesDown, &p.ConnsTotal, &p.CheckAt, &p.CheckResult,
		&p.CheckOK, &p.CheckLatency, &p.CheckCountry, &p.CheckOrg, &p.FailStreak, &p.Label, &p.Scope, &p.CheckCode); err != nil {
		return nil, err
	}
	p.Subnet = uint64(subnet)
	p.IPv4Fallback, p.Enabled = fb == 1, en == 1
	return &p, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type ProxyFilter struct {
	NodeID int64
	IDs    []int64
}

func (s *Store) ListProxies(f ProxyFilter) ([]*Proxy, error) {
	q := `SELECT ` + proxyCols + ` FROM proxies`
	var where []string
	var args []any
	if f.NodeID > 0 {
		where = append(where, "node_id=?")
		args = append(args, f.NodeID)
	}
	if len(f.IDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.IDs)), ",")
		where = append(where, "id IN ("+ph+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := s.DB.Query(q+" ORDER BY node_id, port", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Proxy{}
	for rows.Next() {
		p, err := scanProxy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProxy(id int64) (*Proxy, error) {
	p, err := scanProxy(s.DB.QueryRow(`SELECT `+proxyCols+` FROM proxies WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) UsedPorts(nodeID int64) (map[int]bool, error) {
	rows, err := s.DB.Query(`SELECT port FROM proxies WHERE node_id=?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int]bool{}
	for rows.Next() {
		var p int
		_ = rows.Scan(&p)
		m[p] = true
	}
	return m, rows.Err()
}

func (s *Store) CreateProxies(ps []*Proxy) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range ps {
		p.CreatedAt = Now()
		if p.Scope == "" {
			p.Scope = "lan"
		}
		res, err := tx.Exec(`INSERT INTO proxies(node_id,pool_id,port,protocol,auth_user,auth_pass,subnet,suffix,ipv4_fallback,enabled,note,tag,expires_at,created_at,label,scope)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.NodeID, p.PoolID, p.Port, p.Protocol, p.AuthUser, p.AuthPass, int64(p.Subnet), p.Suffix,
			b2i(p.IPv4Fallback), b2i(p.Enabled), p.Note, p.Tag, p.ExpiresAt, p.CreatedAt, p.Label, p.Scope)
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
	}
	return tx.Commit()
}

func (s *Store) UpdateProxy(p *Proxy) error {
	_, err := s.DB.Exec(`UPDATE proxies SET pool_id=?,port=?,protocol=?,auth_user=?,auth_pass=?,subnet=?,suffix=?,ipv4_fallback=?,enabled=?,note=?,tag=?,expires_at=?,label=?,scope=? WHERE id=?`,
		p.PoolID, p.Port, p.Protocol, p.AuthUser, p.AuthPass, int64(p.Subnet), p.Suffix, b2i(p.IPv4Fallback), b2i(p.Enabled), p.Note, p.Tag, p.ExpiresAt, p.Label, p.Scope, p.ID)
	return err
}

func (s *Store) DeleteProxies(ids []int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM proxies WHERE id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type StatDelta struct {
	Up, Down, Conns int64
}

func (s *Store) AddStats(nodeID int64, d map[int64]StatDelta) error {
	if len(d) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, v := range d {
		if _, err := tx.Exec(`UPDATE proxies SET bytes_up=bytes_up+?,bytes_down=bytes_down+?,conns_total=conns_total+? WHERE id=? AND node_id=?`,
			v.Up, v.Down, v.Conns, id, nodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type CheckRecord struct {
	At      int64  `json:"at"`
	OK      bool   `json:"ok"`
	IP      string `json:"ip"`
	Latency int    `json:"latency"`
	Error   string `json:"error"`
	Code    string `json:"code"`
}

// RecordCheck stores a health-check result and returns the proxy's fail
// streak before and after, so callers can detect state transitions.
func (s *Store) RecordCheck(id int64, c CheckRecord, country, org string) (before, after int, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT fail_streak FROM proxies WHERE id=?`, id).Scan(&before); err != nil {
		return 0, 0, err
	}
	after = 0
	result := "OK " + c.IP
	if !c.OK {
		after = before + 1
		result = "FAIL " + c.Error
	}
	if c.OK {
		_, err = tx.Exec(`UPDATE proxies SET check_at=?,check_result=?,check_ok=1,check_latency=?,check_country=?,check_org=?,fail_streak=0,check_code='' WHERE id=?`,
			c.At, result, c.Latency, country, org, id)
	} else {
		_, err = tx.Exec(`UPDATE proxies SET check_at=?,check_result=?,check_ok=0,check_latency=0,fail_streak=?,check_code=? WHERE id=?`, c.At, result, after, c.Code, id)
	}
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(`INSERT INTO checks(proxy_id,at,ok,ip,latency,error,code) VALUES(?,?,?,?,?,?,?)`, id, c.At, b2i(c.OK), c.IP, c.Latency, c.Error, c.Code); err != nil {
		return 0, 0, err
	}
	_, _ = tx.Exec(`DELETE FROM checks WHERE proxy_id=? AND id <= (SELECT id FROM checks WHERE proxy_id=? ORDER BY id DESC LIMIT 1 OFFSET 200)`, id, id)
	return before, after, tx.Commit()
}

func (s *Store) ListChecks(proxyID int64, limit int) ([]CheckRecord, error) {
	rows, err := s.DB.Query(`SELECT at,ok,ip,latency,error,code FROM checks WHERE proxy_id=? ORDER BY id DESC LIMIT ?`, proxyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CheckRecord{}
	for rows.Next() {
		var c CheckRecord
		var ok int
		if err := rows.Scan(&c.At, &ok, &c.IP, &c.Latency, &c.Error, &c.Code); err != nil {
			return nil, err
		}
		c.OK = ok == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ResetStats(ids []int64) error {
	for _, id := range ids {
		if _, err := s.DB.Exec(`UPDATE proxies SET bytes_up=0,bytes_down=0,conns_total=0 WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}
