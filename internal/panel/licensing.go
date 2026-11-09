package panel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"openv6/internal/license"
	"openv6/internal/store"
)

// ---- openv6 Pro license (Polar one-time license keys) ----

func (s *Server) licState() license.State {
	var st license.State
	if err := json.Unmarshal([]byte(s.Store.GetSetting("license", "")), &st); err != nil || st.Status == "" {
		return license.State{Status: "free"}
	}
	return st
}

func (s *Server) saveLic(st license.State) {
	b, _ := json.Marshal(st)
	_ = s.Store.SetSetting("license", string(b))
}

// pro reports whether Pro features are unlocked.
func (s *Server) pro() bool {
	if s.ForcePro {
		return true
	}
	return s.licState().Pro(time.Now())
}

func (s *Server) machineID() string {
	id := s.Store.GetSetting("machine_id", "")
	if id == "" {
		id = store.RandomToken(12)
		_ = s.Store.SetSetting("machine_id", id)
	}
	return id
}

// needPro writes a 402 with code license_required when Pro is missing.
func (s *Server) needPro(w http.ResponseWriter, what string) bool {
	if s.pro() {
		return false
	}
	writeJSON(w, http.StatusPaymentRequired, apiError{Code: "license_required",
		Error: what + " is an openv6 Pro feature. Free: " + itoa(license.FreeProxies) + " proxies on 1 machine, basic features."})
	return true
}

// freeAllowed returns the proxy ids that may run without a license: the
// first FreeProxies proxies (oldest first) of the first node.
func (s *Server) freeAllowed() (nodeID int64, ids map[int64]bool) {
	ids = map[int64]bool{}
	nodes, _ := s.Store.ListNodes()
	for _, n := range nodes {
		if n.IsLocal {
			nodeID = n.ID
			break
		}
	}
	if nodeID == 0 && len(nodes) > 0 {
		nodeID = nodes[0].ID
	}
	ps, _ := s.Store.ListProxies(store.ProxyFilter{NodeID: nodeID})
	for _, p := range ps {
		if len(ids) >= license.FreeProxies {
			break
		}
		ids[p.ID] = true
	}
	return nodeID, ids
}

func (s *Server) licenseView() map[string]any {
	st := s.licState()
	pro := s.pro()
	host, _ := os.Hostname()
	return map[string]any{
		"pro": pro, "status": st.Status, "machines": st.Machines, "email": st.Email, "display_key": st.DisplayKey,
		"validated_at": st.ValidatedAt, "checked_at": st.CheckedAt, "error": st.Error, "buy_url": license.BuyURL,
		"free_proxies": license.FreeProxies, "free_nodes": license.FreeNodes, "machine": host,
		"grace_days": int(license.Grace.Hours() / 24),
	}
}

func (s *Server) handleGetLicense(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.licenseView())
}

func (s *Server) handleActivateLicense(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		fail(w, 403, "activate the license from the dashboard")
		return
	}
	var b struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	old := s.licState()
	host, _ := os.Hostname()
	label := strings.TrimSpace(host + " (" + runtime.GOOS + ")")
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	st, err := license.Activate(ctx, b.Key, label, s.machineID())
	if err != nil {
		var rej license.ErrRejected
		if errors.As(err, &rej) {
			fail(w, 400, rej.Msg)
		} else {
			fail(w, 502, "could not reach the license server (check the Internet connection): "+err.Error())
		}
		return
	}
	if old.Key != "" && old.ActivationID != "" && (old.Key != st.Key || old.ActivationID != st.ActivationID) {
		_ = license.Deactivate(ctx, old.Key, old.ActivationID) // free the slot of the previous key
	}
	s.saveLic(st)
	s.Store.AddEvent(0, "info", "openv6 Pro activated on this machine")
	writeJSON(w, 200, s.licenseView())
}

func (s *Server) handleDeactivateLicense(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		fail(w, 403, "deactivate the license from the dashboard")
		return
	}
	st := s.licState()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := license.Deactivate(ctx, st.Key, st.ActivationID); err != nil {
		var rej license.ErrRejected
		if !errors.As(err, &rej) { // a key Polar no longer knows can be dropped locally
			fail(w, 502, "could not reach the license server: "+err.Error())
			return
		}
	}
	s.saveLic(license.State{Status: "free"})
	s.Store.AddEvent(0, "info", "openv6 Pro deactivated on this machine")
	writeJSON(w, 200, s.licenseView())
}

// checkLicense re-validates the key once a day (hourly while Polar cannot be
// reached); called from the background loop.
func (s *Server) checkLicense(ctx context.Context) {
	st := s.licState()
	if st.Key == "" {
		return
	}
	every := license.Recheck
	if st.Error != "" && st.Status == "active" {
		every = time.Hour
	}
	if time.Since(time.Unix(st.CheckedAt, 0)) < every {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	was := st.Pro(time.Now())
	st = license.Validate(ctx, st)
	s.saveLic(st)
	if was && !st.Pro(time.Now()) {
		s.Store.AddEvent(0, "warn", "openv6 Pro license is no longer valid: "+st.Error)
	}
}
