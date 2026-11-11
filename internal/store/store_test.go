package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	_ "modernc.org/sqlite"
)

// A database whose settings table has unexpected columns and whose last
// migration was partly applied still opens and keeps its data.
func TestMigrateRepairs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	st.DB.Close()
	db, _ := sql.Open("sqlite", p)
	for _, q := range []string{
		`DROP TABLE settings`,
		`CREATE TABLE settings (k TEXT PRIMARY KEY, v TEXT)`,
		`INSERT INTO settings VALUES ('schema_version','4'), ('lang','en')`,
		`ALTER TABLE proxies DROP COLUMN max_conns`, // pretend migration 5 stopped half-way
		`ALTER TABLE nodes DROP COLUMN upnp`,
		`DROP TABLE api_tokens`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()
	st, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	if st.GetSetting("lang", "") != "en" || st.GetSetting("schema_version", "") != strconv.Itoa(len(migrations)) {
		t.Fatalf("settings lang=%q version=%q", st.GetSetting("lang", ""), st.GetSetting("schema_version", ""))
	}
	if _, _, err := st.CreateAPIToken("t"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListProxies(ProxyFilter{}); err != nil {
		t.Fatal(err)
	}
}

// Losing schema_version must not re-run migration 3, which would turn
// password-protected LAN proxies into Internet proxies.
func TestMissingVersionKeepsScope(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	st, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := st.CreateNode("n", "", true)
	if err := st.CreatePool(&Pool{NodeID: n.ID, Name: "a", CIDR: "2001:db8::/64", Mode: "addr"}); err != nil {
		t.Fatal(err)
	}
	pools, _ := st.ListPools(n.ID)
	if err := st.CreateProxies([]*Proxy{{NodeID: n.ID, PoolID: pools[0].ID, Port: 10001, Protocol: "both", AuthUser: "u", AuthPass: "p", Suffix: "::1", Enabled: true, Scope: "lan"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = st.DB.Exec(`DELETE FROM settings WHERE key='schema_version'`)
	st.DB.Close()
	st, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	ps, _ := st.ListProxies(ProxyFilter{})
	if len(ps) != 1 || ps[0].Scope != "lan" {
		t.Fatalf("scope changed: %+v", ps[0].Scope)
	}
}

func TestSecretsEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := st.CreateNode("n", "", true)
	_ = st.CreatePool(&Pool{NodeID: n.ID, Name: "a", CIDR: "2001:db8::/64", Mode: "addr"})
	pools, _ := st.ListPools(n.ID)
	_ = st.CreateProxies([]*Proxy{{NodeID: n.ID, PoolID: pools[0].ID, Port: 10001, Protocol: "both", AuthUser: "u", AuthPass: "s3cret-pass", Suffix: "::1", Enabled: true}})
	_ = st.SetSetting("telegram_token", "123:abc")
	_ = st.SetSetting("license", `{"key":"OV6"}`)
	var raw, rawTok string
	_ = st.DB.QueryRow(`SELECT auth_pass FROM proxies`).Scan(&raw)
	_ = st.DB.QueryRow(`SELECT value FROM settings WHERE key='telegram_token'`).Scan(&rawTok)
	if raw == "s3cret-pass" || rawTok == "123:abc" {
		t.Fatal("secrets stored in plaintext")
	}
	ps, _ := st.ListProxies(ProxyFilter{})
	if ps[0].AuthPass != "s3cret-pass" || st.GetSetting("telegram_token", "") != "123:abc" {
		t.Fatal("decryption failed")
	}
	b, err := st.Dump("t")
	if err != nil {
		t.Fatal(err)
	}
	if b.Tables["proxies"][0]["auth_pass"] != "s3cret-pass" {
		t.Fatalf("backup not plaintext: %v", b.Tables["proxies"][0]["auth_pass"])
	}
	for _, r := range b.Tables["settings"] {
		if r["key"] == "license" {
			t.Fatal("license exported in backup")
		}
	}
	// restore into another installation (different master.key)
	st2, err := Open(filepath.Join(t.TempDir(), "y.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st2.CreateUser("admin", "password123", "admin")
	b.Tables["users"] = []map[string]any{{"id": float64(1), "username": "admin", "pass_hash": "x", "role": "admin", "created_at": float64(1)}}
	if err := st2.Restore(b); err != nil {
		t.Fatal(err)
	}
	ps2, _ := st2.ListProxies(ProxyFilter{})
	_ = st2.DB.QueryRow(`SELECT auth_pass FROM proxies`).Scan(&raw)
	if ps2[0].AuthPass != "s3cret-pass" || raw == "s3cret-pass" {
		t.Fatalf("restore: %q raw %q", ps2[0].AuthPass, raw)
	}
}

// Losing master.key must lock passwords, never turn them into "no password".
func TestLostKeyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(filepath.Join(dir, "x.db"))
	n, _ := st.CreateNode("n", "", true)
	_ = st.CreatePool(&Pool{NodeID: n.ID, Name: "a", CIDR: "2001:db8::/64", Mode: "addr"})
	pools, _ := st.ListPools(n.ID)
	_ = st.CreateProxies([]*Proxy{{NodeID: n.ID, PoolID: pools[0].ID, Port: 10001, Protocol: "both", AuthUser: "u", AuthPass: "pw", Suffix: "::1", Enabled: true}})
	st.DB.Close()
	if err := os.WriteFile(filepath.Join(dir, "master.key"), []byte("short"), 0o600); err != nil { // damaged key
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatalf("service must still start: %v", err)
	}
	defer st.DB.Close()
	ps, _ := st.ListProxies(ProxyFilter{})
	if ps[0].AuthPass == "" || ps[0].AuthPass == "pw" {
		t.Fatalf("password not locked: %q", ps[0].AuthPass)
	}
}

// Where the IP is (time zone, city, coordinates) is kept while the proxy
// stays in its pool and forgotten when it moves to another prefix.
func TestGeoClearedOnPoolChange(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := st.CreateNode("n", "", true)
	_ = st.CreatePool(&Pool{NodeID: n.ID, Name: "a", CIDR: "2001:db8:1::/64", Mode: "addr"})
	_ = st.CreatePool(&Pool{NodeID: n.ID, Name: "b", CIDR: "2001:db8:2::/64", Mode: "addr"})
	pools, _ := st.ListPools(n.ID)
	_ = st.CreateProxies([]*Proxy{{NodeID: n.ID, PoolID: pools[0].ID, Port: 10001, Protocol: "both", Suffix: "::1", Enabled: true}})
	ps, _ := st.ListProxies(ProxyFilter{})
	p := ps[0]
	if _, _, err := st.RecordCheck(p.ID, CheckRecord{At: 1, OK: true, Latency: 10, IP: "2001:db8:1::1"}, Geo{Country: "VN", Timezone: "Asia/Ho_Chi_Minh", City: "Hanoi", Loc: "21.0,105.8"}); err != nil {
		t.Fatal(err)
	}
	p, _ = st.GetProxy(p.ID)
	p.Note = "renamed"
	_ = st.UpdateProxy(p)
	if p, _ = st.GetProxy(p.ID); p.CheckTZ != "Asia/Ho_Chi_Minh" || p.CheckCity != "Hanoi" || p.CheckLoc != "21.0,105.8" {
		t.Fatalf("geo lost on a plain edit: %q %q %q", p.CheckTZ, p.CheckCity, p.CheckLoc)
	}
	p.PoolID = pools[1].ID
	_ = st.UpdateProxy(p)
	if p, _ = st.GetProxy(p.ID); p.CheckTZ != "" || p.CheckCity != "" || p.CheckLoc != "" {
		t.Fatalf("geo kept after moving to another pool: %q %q %q", p.CheckTZ, p.CheckCity, p.CheckLoc)
	}
}
