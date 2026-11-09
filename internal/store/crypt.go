package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Secrets at rest: proxy passwords and sensitive settings are stored
// encrypted (AES-256-GCM). The key is master.key next to the database, in
// the data folder only root/SYSTEM can read; a copied database or a stray
// file listing leaks no credentials. Backups contain plaintext (they are
// meant to move to another machine, which has another key).

const encPrefix = "enc1:"

// sensitive settings are encrypted; everything else stays readable.
var sensitiveSettings = map[string]bool{"telegram_token": true, "license": true}

func loadKey(dbPath string) (cipher.AEAD, error) {
	p := filepath.Join(filepath.Dir(dbPath), "master.key")
	key, err := os.ReadFile(p)
	if err == nil && len(key) != 32 {
		// damaged (e.g. power cut while writing): keep it aside and start a new
		// key; secrets it protected stay locked (fail closed) until reset
		log.Printf("store: master.key is damaged; moved to master.key.bad, stored proxy passwords must be reset")
		_ = os.Rename(p, p+".bad")
		err = os.ErrNotExist
	}
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := writeFileSync(p, key); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

func (s *Store) enc(v string) string {
	if v == "" || s.aead == nil {
		return v
	}
	nonce := make([]byte, s.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return encPrefix + base64.StdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(v), nil))
}

// dec returns plaintext; values written before encryption pass through.
func (s *Store) dec(v string) string {
	if !strings.HasPrefix(v, encPrefix) || s.aead == nil {
		return v
	}
	raw, err := base64.StdEncoding.DecodeString(v[len(encPrefix):])
	n := s.aead.NonceSize()
	if err == nil && len(raw) >= n {
		if out, err := s.aead.Open(nil, raw[:n], raw[n:], nil); err == nil {
			return string(out)
		}
	}
	// fail closed: never turn an unreadable password into "no password";
	// a random one nobody knows keeps the proxy locked until it is reset
	log.Printf("store: cannot decrypt a stored secret (master.key replaced or missing?); reset the proxy password")
	return "locked-" + RandomToken(16)
}

// EncryptExisting encrypts plaintext secrets (older versions, restores).
func (s *Store) EncryptExisting() error {
	if s.aead == nil {
		return nil
	}
	rows, err := s.DB.Query(`SELECT id, auth_pass FROM proxies WHERE auth_pass <> '' AND auth_pass NOT LIKE 'enc1:%'`)
	if err != nil {
		return err
	}
	type row struct {
		id int64
		v  string
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.v); err == nil {
			todo = append(todo, r)
		}
	}
	rows.Close()
	if len(todo) > 0 {
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		for _, r := range todo {
			if _, err := tx.Exec(`UPDATE proxies SET auth_pass=? WHERE id=?`, s.enc(r.v), r.id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	for k := range sensitiveSettings {
		var v string
		if s.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, k).Scan(&v) == nil && v != "" && !strings.HasPrefix(v, encPrefix) {
			if _, err := s.DB.Exec(`UPDATE settings SET value=? WHERE key=?`, s.enc(v), k); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeFileSync writes atomically: temp file, fsync, rename.
func writeFileSync(p string, b []byte) error {
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
