package panel

import (
	"crypto/subtle"
	"net/http"
	"net/netip"
	"time"

	"openv6/internal/proto"
	"openv6/internal/store"
	"openv6/internal/update"
)

// Tray endpoints are used by the system tray app running as the logged-in
// user. They only accept loopback connections carrying the tray token, which
// the service writes to a file readable by local users.

func (s *Server) trayAuth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ap, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil || !ap.Addr().Unmap().IsLoopback() || s.TrayToken == "" ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Tray-Token")), []byte(s.TrayToken)) != 1 {
			fail(w, http.StatusForbidden, "tray access only from this machine")
			return
		}
		h(w, r)
	})
}

type trayStatus struct {
	Version    string `json:"version"`
	Lang       string `json:"lang"`
	Online     bool   `json:"online"`
	Paused     bool   `json:"paused"`
	Total      int    `json:"total"`
	OK         int    `json:"ok"`
	Failing    int    `json:"failing"`
	Errors     int    `json:"errors"`
	Prefix     string `json:"prefix"`
	Problem    string `json:"problem,omitempty"` // first failing self-check id
	NewVersion string `json:"new_version,omitempty"`
	Setup      bool   `json:"setup_required"`
}

func (s *Server) localNode() (*store.Node, error) { return s.Store.LocalNode() }

func (s *Server) handleTrayStatus(w http.ResponseWriter, r *http.Request) {
	out := trayStatus{Version: proto.Version, Lang: s.lang(), Setup: s.Store.UserCount() == 0}
	n, err := s.localNode()
	if err == nil {
		out.Online = time.Now().Unix()-n.LastSeen <= onlineWindow
		out.Paused = n.Paused
		vs, _ := s.proxyViews(store.ProxyFilter{NodeID: n.ID})
		for _, v := range vs {
			if !v.Enabled {
				continue
			}
			out.Total++
			switch {
			case v.Status == "ok" && !(v.CheckOK == 0 && v.FailStreak >= 2):
				out.OK++
			case v.Status == "error":
				out.Errors++
			}
			if v.CheckOK == 0 && v.FailStreak >= 2 {
				out.Failing++
			}
		}
		pools, _ := s.Store.ListPools(n.ID)
		for _, p := range pools {
			if p.Auto {
				out.Prefix = p.CIDR
			}
		}
		nv := s.nodeView(n, nil)
		for _, c := range nv.Checks {
			if !c.OK && !c.Warn {
				out.Problem = c.ID
				break
			}
		}
	}
	s.mon.mu.Lock()
	if s.mon.latest != nil && update.Newer(s.mon.latest.Tag, proto.Version) {
		out.NewVersion = s.mon.latest.Tag
	}
	s.mon.mu.Unlock()
	writeJSON(w, 200, out)
}

func (s *Server) handleTrayAction(w http.ResponseWriter, r *http.Request) {
	n, err := s.localNode()
	if err != nil {
		fail(w, 404, "no local node")
		return
	}
	switch r.PathValue("action") {
	case "pause", "resume":
		paused := r.PathValue("action") == "pause"
		if err := s.Store.SetNodePaused(n.ID, paused); err != nil {
			logErr(w, err)
			return
		}
		msg := "proxies resumed"
		if paused {
			msg = "proxies paused"
		}
		s.Store.AddEvent(n.ID, "info", msg+" (tray)")
	case "check":
		s.queueChecks(store.ProxyFilter{NodeID: n.ID})
	case "reapply":
		s.queueCommand(n.ID, proto.Command{Type: "reapply"})
	case "lang-vi", "lang-en":
		_ = s.Store.SetSetting("lang", r.PathValue("action")[5:])
	default:
		fail(w, 400, "unknown action")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTrayLogin returns a one-time link that signs the browser in as the
// first admin, so "Open dashboard" from the tray needs no password.
func (s *Server) handleTrayLogin(w http.ResponseWriter, r *http.Request) {
	base := "http://127.0.0.1:" + itoa(s.ListenPort)
	if s.Store.UserCount() == 0 {
		writeJSON(w, 200, map[string]string{"url": base + "/"})
		return
	}
	tok := store.RandomToken(24)
	s.cmdMu.Lock()
	s.magic[tok] = time.Now().Add(60 * time.Second)
	s.cmdMu.Unlock()
	writeJSON(w, 200, map[string]string{"url": base + "/auth/magic?t=" + tok})
}

func (s *Server) handleMagic(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("t")
	s.cmdMu.Lock()
	exp, ok := s.magic[tok]
	delete(s.magic, tok)
	for k, v := range s.magic {
		if time.Now().After(v) {
			delete(s.magic, k)
		}
	}
	s.cmdMu.Unlock()
	dest := "/"
	switch next := r.URL.Query().Get("next"); next {
	case "proxies", "nodes", "settings", "events":
		dest = "/#" + next
	}
	if !ok || time.Now().After(exp) {
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}
	var uid int64
	_ = s.Store.DB.QueryRow(`SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&uid)
	sess, err := s.Store.CreateSession(uid, sessionTTL)
	if err == nil {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sess, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteLaxMode, MaxAge: sessionTTL})
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) handleNodePause(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	var b struct {
		Paused bool `json:"paused"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err := s.Store.SetNodePaused(n.ID, b.Paused); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
