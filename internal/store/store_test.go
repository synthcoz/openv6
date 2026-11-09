package store

import (
	"database/sql"
	"path/filepath"
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
	if st.GetSetting("lang", "") != "en" || st.GetSetting("schema_version", "") != "5" {
		t.Fatalf("settings lang=%q version=%q", st.GetSetting("lang", ""), st.GetSetting("schema_version", ""))
	}
	if _, _, err := st.CreateAPIToken("t"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListProxies(ProxyFilter{}); err != nil {
		t.Fatal(err)
	}
}
