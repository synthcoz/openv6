package store

import (
	"fmt"
	"sort"
	"strings"
)

// Tables included in a backup, in insert order (foreign keys).
var backupTables = []string{"settings", "users", "nodes", "pools", "proxies"}

type Backup struct {
	Format     int                         `json:"format"`
	AppVersion string                      `json:"app_version"`
	CreatedAt  int64                       `json:"created_at"`
	Tables     map[string][]map[string]any `json:"tables"`
}

// Dump exports configuration tables (not sessions, events or check history).
func (s *Store) Dump(appVersion string) (*Backup, error) {
	b := &Backup{Format: 1, AppVersion: appVersion, CreatedAt: Now(), Tables: map[string][]map[string]any{}}
	for _, t := range backupTables {
		rows, err := s.DB.Query("SELECT * FROM " + t)
		if err != nil {
			return nil, err
		}
		cols, _ := rows.Columns()
		list := []map[string]any{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			m := map[string]any{}
			for i, c := range cols {
				if t == "settings" && c == "key" && vals[i] == "schema_version" {
					m = nil
					break
				}
				if bs, ok := vals[i].([]byte); ok {
					vals[i] = string(bs)
				}
				m[c] = vals[i]
			}
			if m != nil {
				list = append(list, m)
			}
		}
		rows.Close()
		b.Tables[t] = list
	}
	return b, nil
}

// Restore replaces all configuration with the backup content. Columns that do
// not exist in the current schema are ignored; missing ones get defaults.
func (s *Store) Restore(b *Backup) error {
	if b.Format != 1 {
		return fmt.Errorf("unsupported backup format %d", b.Format)
	}
	if len(b.Tables["users"]) == 0 {
		return fmt.Errorf("backup has no users; refusing to lock you out")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := len(backupTables) - 1; i >= 0; i-- {
		t := backupTables[i]
		q := "DELETE FROM " + t
		if t == "settings" {
			q += " WHERE key <> 'schema_version'"
		}
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	for _, t := range []string{"sessions", "checks", "events"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	for _, t := range backupTables {
		valid := map[string]bool{}
		rows, err := tx.Query("SELECT name FROM pragma_table_info(?)", t)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			valid[n] = true
		}
		rows.Close()
		for _, row := range b.Tables[t] {
			var cols []string
			for c := range row {
				if valid[c] && !(t == "settings" && row["key"] == "schema_version") {
					cols = append(cols, c)
				}
			}
			if len(cols) == 0 {
				continue
			}
			sort.Strings(cols)
			args := make([]any, len(cols))
			for i, c := range cols {
				args[i] = row[c]
				if f, ok := args[i].(float64); ok && f == float64(int64(f)) {
					args[i] = int64(f) // JSON numbers come back as float64
				}
			}
			q := fmt.Sprintf("INSERT INTO %s(%s) VALUES(%s)", t, strings.Join(cols, ","), strings.TrimSuffix(strings.Repeat("?,", len(cols)), ","))
			if _, err := tx.Exec(q, args...); err != nil {
				return fmt.Errorf("restore %s: %w", t, err)
			}
		}
	}
	return tx.Commit()
}
