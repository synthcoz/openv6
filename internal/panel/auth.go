package panel

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openv6/internal/store"
)

const sessionCookie = "v6p_session"
const sessionTTL = 7 * 24 * 3600

type ctxKey int

const userKey ctxKey = 1
const apiKey ctxKey = 2

func isAPIRequest(r *http.Request) bool { v, _ := r.Context().Value(apiKey).(bool); return v }

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API tokens (scripts, integrations): Authorization: Bearer ov6_…
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && strings.HasPrefix(tok, "ov6_") {
			ip := clientIP(r)
			if s.tooManyFailures(ip) {
				fail(w, http.StatusTooManyRequests, "too many failed attempts, wait 5 minutes")
				return
			}
			if !s.pro() {
				fail(w, http.StatusPaymentRequired, "API tokens need openv6 Pro")
				return
			}
			if !s.Store.CheckAPIToken(strings.TrimSpace(tok)) {
				s.recordFailure(ip)
				fail(w, http.StatusUnauthorized, "invalid API token")
				return
			}
			ctx := context.WithValue(r.Context(), userKey, &store.User{Username: "api", Role: "admin"})
			h(w, r.WithContext(context.WithValue(ctx, apiKey, true)))
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			fail(w, http.StatusUnauthorized, "login required")
			return
		}
		u, err := s.Store.SessionUser(c.Value)
		if err != nil {
			fail(w, http.StatusUnauthorized, "login required")
			return
		}
		// CSRF: state-changing requests must be JSON (cannot be sent cross-site
		// without a CORS preflight) or carry our custom header.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") && r.Header.Get("X-Requested-With") == "" {
				fail(w, http.StatusForbidden, "bad request origin")
				return
			}
		}
		if u.Role == "customer" && !customerRoutes[r.Pattern] {
			fail(w, http.StatusForbidden, "not available for customer accounts")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// customerRoutes are the only endpoints customer accounts (reseller mode)
// may use; handlers further limit them to their own proxies.
var customerRoutes = map[string]bool{
	"GET /api/proxies": true, "POST /api/proxies/export": true, "POST /api/proxies/action": true,
	"GET /api/commands/{id}": true, "POST /api/account/password": true, "GET /api/proxies/{id}/checks": true,
}

func isCustomer(r *http.Request) bool {
	u := currentUser(r)
	return u != nil && u.Role == "customer"
}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	setup := s.Store.UserCount() == 0
	out := map[string]any{"setup_required": setup, "user": nil, "setup_code_required": setup && !s.setupTrusted(r)}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, err := s.Store.SessionUser(c.Value); err == nil {
			out["user"] = u
		}
	}
	writeJSON(w, 200, out)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code,omitempty"` // first-time setup code (remote setup only)
}

// setupTrusted: setup without a code is allowed from this machine or the
// local network, addressed by IP or a local name (blocks DNS rebinding).
func (s *Server) setupTrusted(r *http.Request) bool {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	a := ap.Addr().Unmap()
	if !(a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || cgnatNet.Contains(a)) {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return host == "localhost" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home") || strings.HasSuffix(host, ".ts.net")
}

var cgnatNet = netip.MustParsePrefix("100.64.0.0/10")

// SetupCode returns the one-time code for remote first-time setup, creating
// it (and setup-code.txt, readable by root/admins only) when needed.
func (s *Server) SetupCode() string {
	if s.Store.UserCount() > 0 {
		return ""
	}
	code := s.Store.GetSetting("setup_code", "")
	if code == "" {
		code = randDigits(8)
		_ = s.Store.SetSetting("setup_code", code)
	}
	_ = os.WriteFile(filepath.Join(s.DataDir, "setup-code.txt"), []byte(code+"\n"), 0o600)
	return code
}

func randDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(10))
		b[i] = byte('0' + k.Int64())
	}
	return string(b)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.Store.UserCount() > 0 {
		fail(w, http.StatusConflict, "already set up")
		return
	}
	var c credentials
	if err := readJSON(r, &c); err != nil {
		fail(w, 400, err.Error())
		return
	}
	// a fresh panel on a VPS must not be claimable by whoever finds it first
	if !s.setupTrusted(r) {
		ip := clientIP(r)
		if s.tooManyFailures(ip) {
			fail(w, http.StatusTooManyRequests, "too many failed attempts, wait 5 minutes")
			return
		}
		want := s.SetupCode()
		if want == "" || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(c.Code)), []byte(want)) != 1 {
			s.recordFailure(ip)
			fail(w, http.StatusForbidden, "setup code required: it is printed by the installer and in the log ("+filepath.Join(s.DataDir, "setup-code.txt")+")")
			return
		}
	}
	c.Username = strings.TrimSpace(c.Username)
	if c.Username == "" || len(c.Password) < 8 {
		fail(w, 400, "username required and password must be at least 8 characters")
		return
	}
	if err := s.Store.CreateUser(c.Username, c.Password, "admin"); err != nil {
		logErr(w, err)
		return
	}
	s.Store.AddEvent(0, "info", "admin account created: "+c.Username)
	_, _ = s.Store.DB.Exec(`DELETE FROM settings WHERE key='setup_code'`)
	_ = os.Remove(filepath.Join(s.DataDir, "setup-code.txt"))
	s.startSession(w, r, c)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		fail(w, 400, err.Error())
		return
	}
	ip := clientIP(r)
	if s.tooManyFailures(ip) {
		fail(w, http.StatusTooManyRequests, "too many failed attempts, wait 5 minutes")
		return
	}
	s.startSession(w, r, c)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, c credentials) {
	u, err := s.Store.CheckLogin(c.Username, c.Password)
	if err != nil {
		s.recordFailure(clientIP(r))
		fail(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	tok, err := s.Store.CreateSession(u.ID, sessionTTL)
	if err != nil {
		logErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: sessionTTL})
	writeJSON(w, 200, map[string]any{"user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.Store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	u := currentUser(r)
	if _, err := s.Store.CheckLogin(u.Username, b.Current); err != nil {
		fail(w, 400, "current password is wrong")
		return
	}
	if len(b.New) < 8 {
		fail(w, 400, "password must be at least 8 characters")
		return
	}
	if err := s.Store.SetPassword(u.Username, b.New); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// failKey groups IPv6 clients by /64: one home or VPS gets a whole /64, so
// counting single addresses would be trivial to bypass.
func failKey(ip string) string {
	if a, err := netip.ParseAddr(ip); err == nil && a.Unmap().Is6() {
		p, _ := a.Prefix(64)
		return p.String()
	}
	return ip
}

func (s *Server) tooManyFailures(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	cut := time.Now().Add(-5 * time.Minute)
	total := 0
	for k, ts := range s.failures { // prune: the map never grows without bound
		var keep []time.Time
		for _, t := range ts {
			if t.After(cut) {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(s.failures, k)
			continue
		}
		s.failures[k] = keep
		total += len(keep)
	}
	return len(s.failures[failKey(ip)]) >= 10 || total >= 200
}

func (s *Server) recordFailure(ip string) {
	s.loginMu.Lock()
	k := failKey(ip)
	s.failures[k] = append(s.failures[k], time.Now())
	s.loginMu.Unlock()
}
