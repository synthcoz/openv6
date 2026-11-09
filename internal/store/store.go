// Package store persists panel data in SQLite.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

var ErrNotFound = errors.New("not found")

const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY, username TEXT UNIQUE NOT NULL, pass_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin', created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS nodes (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, token TEXT UNIQUE NOT NULL,
  entry_host TEXT NOT NULL DEFAULT '', iface TEXT NOT NULL DEFAULT '',
  allow_cidrs TEXT NOT NULL DEFAULT '', is_local INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER NOT NULL DEFAULT 0, state_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS pools (
  id INTEGER PRIMARY KEY, node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  name TEXT NOT NULL, auto INTEGER NOT NULL DEFAULT 0, cidr TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT 'addr', created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS proxies (
  id INTEGER PRIMARY KEY, node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  pool_id INTEGER NOT NULL REFERENCES pools(id) ON DELETE CASCADE,
  port INTEGER NOT NULL, protocol TEXT NOT NULL DEFAULT 'both',
  auth_user TEXT NOT NULL DEFAULT '', auth_pass TEXT NOT NULL DEFAULT '',
  subnet INTEGER NOT NULL DEFAULT 0, suffix TEXT NOT NULL,
  ipv4_fallback INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1,
  note TEXT NOT NULL DEFAULT '', tag TEXT NOT NULL DEFAULT '',
  expires_at INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
  bytes_up INTEGER NOT NULL DEFAULT 0, bytes_down INTEGER NOT NULL DEFAULT 0,
  conns_total INTEGER NOT NULL DEFAULT 0,
  check_at INTEGER NOT NULL DEFAULT 0, check_result TEXT NOT NULL DEFAULT '',
  UNIQUE(node_id, port));
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, node_id INTEGER NOT NULL DEFAULT 0,
  level TEXT NOT NULL, message TEXT NOT NULL);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	st := &Store{DB: db}
	if err := st.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return st, nil
}

// migrations are applied in order; never edit or reorder an existing entry.
var migrations = []string{
	// 1: health checks
	`ALTER TABLE proxies ADD COLUMN check_ok INTEGER NOT NULL DEFAULT -1;
	 ALTER TABLE proxies ADD COLUMN check_latency INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE proxies ADD COLUMN check_country TEXT NOT NULL DEFAULT '';
	 ALTER TABLE proxies ADD COLUMN check_org TEXT NOT NULL DEFAULT '';
	 ALTER TABLE proxies ADD COLUMN fail_streak INTEGER NOT NULL DEFAULT 0;
	 CREATE TABLE IF NOT EXISTS checks (
	   id INTEGER PRIMARY KEY, proxy_id INTEGER NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
	   at INTEGER NOT NULL, ok INTEGER NOT NULL, ip TEXT NOT NULL DEFAULT '', latency INTEGER NOT NULL DEFAULT 0,
	   error TEXT NOT NULL DEFAULT '');
	 CREATE INDEX IF NOT EXISTS checks_proxy ON checks(proxy_id, at);`,
	// 2: profile name shown as the main label of a proxy
	`ALTER TABLE proxies ADD COLUMN label TEXT NOT NULL DEFAULT '';`,
	// 3: sharing scope, VPN bypass, per-node DNS, error codes
	`ALTER TABLE proxies ADD COLUMN scope TEXT NOT NULL DEFAULT 'lan';
	 UPDATE proxies SET scope='any' WHERE auth_user <> '';
	 ALTER TABLE proxies ADD COLUMN check_code TEXT NOT NULL DEFAULT '';
	 ALTER TABLE checks ADD COLUMN code TEXT NOT NULL DEFAULT '';
	 ALTER TABLE nodes ADD COLUMN bypass INTEGER NOT NULL DEFAULT 1;
	 ALTER TABLE nodes ADD COLUMN dns TEXT NOT NULL DEFAULT '';`,
}

func (s *Store) migrate() error {
	v, _ := strconv.Atoi(s.GetSetting("schema_version", "0"))
	for i := v; i < len(migrations); i++ {
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES('schema_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func Now() int64 { return time.Now().Unix() }

func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- settings ----

func (s *Store) GetSetting(k, def string) string {
	var v string
	if err := s.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, k).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(k, v string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

// ---- events ----

type Event struct {
	ID      int64  `json:"id"`
	At      int64  `json:"at"`
	NodeID  int64  `json:"node_id"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

func (s *Store) AddEvent(nodeID int64, level, msg string) {
	_, _ = s.DB.Exec(`INSERT INTO events(at,node_id,level,message) VALUES(?,?,?,?)`, Now(), nodeID, level, msg)
	_, _ = s.DB.Exec(`DELETE FROM events WHERE id <= (SELECT id FROM events ORDER BY id DESC LIMIT 1 OFFSET 1000)`)
}

func (s *Store) ListEvents(limit int) ([]Event, error) {
	rows, err := s.DB.Query(`SELECT id,at,node_id,level,message FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.NodeID, &e.Level, &e.Message); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
