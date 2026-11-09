package panel

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"openv6/internal/store"
)

const sessionCookie = "v6p_session"
const sessionTTL = 7 * 24 * 3600

type ctxKey int

const userKey ctxKey = 1

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		h(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
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
	out := map[string]any{"setup_required": s.Store.UserCount() == 0, "user": nil}
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

func (s *Server) tooManyFailures(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	cut := time.Now().Add(-5 * time.Minute)
	var keep []time.Time
	for _, t := range s.failures[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	s.failures[ip] = keep
	return len(keep) >= 10
}

func (s *Server) recordFailure(ip string) {
	s.loginMu.Lock()
	s.failures[ip] = append(s.failures[ip], time.Now())
	s.loginMu.Unlock()
}
