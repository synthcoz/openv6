// Package panel serves the management web UI, the REST API used by the UI and
// the sync endpoint used by agents.
package panel

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"openv6/internal/logbuf"
	"openv6/internal/store"
)

type Server struct {
	Store      *store.Store
	WebFS      fs.FS
	DataDir    string
	ListenPort int
	SelfExe    string // path of the running binary, served to agents for install

	cmdMu    sync.Mutex
	licMu    sync.Mutex
	pending  map[int64][]pendingCmd   // node id -> queued commands
	results  map[string]*commandState // command id -> state
	loginMu  sync.Mutex
	failures map[string][]time.Time // ip -> recent failed logins
	stateMu  sync.RWMutex
	live     map[int64]*liveState     // node id -> last reported live data
	logs     map[int64]*logbuf.Buffer // node id -> recent agent log lines
	mon      monitor

	// PanelLogs holds this process's own log output (panel + local agent).
	PanelLogs *logbuf.Buffer
	// OnShutdown stops the whole server (tray "Quit").
	OnShutdown func()
	// TrayToken authorizes the local tray app (see tray.go).
	TrayToken string
	// ForcePro unlocks Pro features without a license (tests, development).
	ForcePro bool
	magic    map[string]time.Time // one-time login links from the tray
}

func New(st *store.Store, web fs.FS, dataDir string, port int, exe string) *Server {
	return &Server{
		Store: st, WebFS: web, DataDir: dataDir, ListenPort: port, SelfExe: exe,
		pending: map[int64][]pendingCmd{}, results: map[string]*commandState{},
		failures: map[string][]time.Time{}, live: map[int64]*liveState{}, logs: map[int64]*logbuf.Buffer{},
		mon: monitor{nodeOnline: map[int64]bool{}}, magic: map[string]time.Time{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// public
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("POST /api/agent/sync", s.handleAgentSync)
	mux.HandleFunc("GET /install.sh", s.handleInstallScript)
	mux.HandleFunc("GET /download/SHA256SUMS", s.handleSums)
	mux.HandleFunc("GET /download/SHA256SUMS.sig", s.handleSums)
	mux.HandleFunc("GET /download/{arch}", s.handleDownload)
	mux.HandleFunc("GET /download/{os}/{arch}", s.handleDownload)
	mux.HandleFunc("GET /install.ps1", s.handleInstallPS1)
	mux.HandleFunc("GET /auth/magic", s.handleMagic)
	mux.Handle("GET /api/tray/status", s.trayAuth(s.handleTrayStatus))
	mux.Handle("POST /api/tray/login", s.trayAuth(s.handleTrayLogin))
	mux.Handle("POST /api/tray/{action}", s.trayAuth(s.handleTrayAction))

	// authenticated
	a := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h)) }
	a("GET /api/dashboard", s.handleDashboard)
	a("GET /api/nodes", s.handleListNodes)
	a("POST /api/nodes", s.handleCreateNode)
	a("PUT /api/nodes/{id}", s.handleUpdateNode)
	a("DELETE /api/nodes/{id}", s.handleDeleteNode)
	a("POST /api/nodes/{id}/rotate-token", s.handleRotateToken)
	a("POST /api/nodes/{id}/test", s.handleTestNode)
	a("GET /api/nodes/{id}/install", s.handleInstallCmd)
	a("GET /api/pools", s.handleListPools)
	a("POST /api/pools", s.handleCreatePool)
	a("PUT /api/pools/{id}", s.handleUpdatePool)
	a("DELETE /api/pools/{id}", s.handleDeletePool)
	a("GET /api/proxies", s.handleListProxies)
	a("POST /api/proxies/bulk", s.handleBulkCreate)
	a("PUT /api/proxies/{id}", s.handleUpdateProxy)
	a("POST /api/proxies/action", s.handleProxyAction)
	a("POST /api/proxies/{id}/test-site", s.handleTestSite)
	a("POST /api/proxies/export", s.handleExport)
	a("GET /api/commands/{id}", s.handleGetCommand)
	a("GET /api/events", s.handleEvents)
	a("GET /api/settings", s.handleGetSettings)
	a("PUT /api/settings", s.handlePutSettings)
	a("POST /api/account/password", s.handleChangePassword)
	a("POST /api/settings/telegram-test", s.handleTelegramTest)
	a("GET /api/update", s.handleUpdateInfo)
	a("POST /api/update/panel", s.handleUpdatePanel)
	a("POST /api/update/nodes", s.handleUpdateNodes)
	a("POST /api/nodes/{id}/update", s.handleUpdateNodes)
	a("POST /api/nodes/{id}/restart", s.handleNodeRestart)
	a("POST /api/nodes/{id}/reapply", s.handleNodeReapply)
	a("POST /api/nodes/{id}/fix", s.handleNodeFix)
	a("POST /api/nodes/{id}/pause", s.handleNodePause)
	a("GET /api/backup", s.handleBackup)
	a("POST /api/restore", s.handleRestore)
	a("GET /api/nodes/{id}/logs", s.handleNodeLogs)
	a("GET /api/proxies/{id}/checks", s.handleProxyChecks)
	a("POST /api/checks/run", s.handleCheckNow)
	a("GET /api/users", s.handleListUsers)
	a("POST /api/users", s.handleCreateUser)
	a("PUT /api/users/{id}", s.handleUpdateUser)
	a("DELETE /api/users/{id}", s.handleDeleteUser)
	a("GET /api/license", s.handleGetLicense)
	a("POST /api/license", s.handleActivateLicense)
	a("DELETE /api/license", s.handleDeactivateLicense)
	a("GET /api/tokens", s.handleListTokens)
	a("POST /api/tokens", s.handleCreateToken)
	a("DELETE /api/tokens/{id}", s.handleDeleteToken)

	// static UI
	mux.Handle("GET /", &staticHandler{fsys: s.WebFS})
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

func readJSON(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("content-type must be application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	return dec.Decode(v)
}

func logErr(w http.ResponseWriter, err error) {
	log.Printf("panel: %v", err)
	fail(w, http.StatusInternalServerError, err.Error())
}
