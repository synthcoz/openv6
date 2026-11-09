package store

import (
	"database/sql"
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
