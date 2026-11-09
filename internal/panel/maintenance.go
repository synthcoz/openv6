package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"openv6/internal/logbuf"
	"openv6/internal/proto"
	"openv6/internal/store"
	"openv6/internal/update"
)

// ---- updates ----

func (s *Server) handleUpdateInfo(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.checkForUpdate(r.Context())
	}
	s.mon.mu.Lock()
	rel, errStr, updating := s.mon.latest, s.mon.latestErr, s.mon.updating
	s.mon.mu.Unlock()
	out := map[string]any{"current": proto.Version, "updating": updating, "error": errStr,
		"managed": os.Getenv("INVOCATION_ID") != "", "repo": s.updateRepo()}
	if rel != nil {
		out["latest"] = rel.Tag
		out["notes"] = rel.Notes
		out["url"] = rel.URL
		out["newer"] = update.Newer(rel.Tag, proto.Version)
	}
	nodes, _ := s.Store.ListNodes()
	var outdated []string
	for _, n := range nodes {
		var req proto.SyncRequest
		_ = json.Unmarshal([]byte(n.StateJSON), &req)
		if !n.IsLocal && req.AgentVersion != "" && req.AgentVersion != proto.Version {
			outdated = append(outdated, n.Name)
		}
	}
	out["outdated_nodes"] = outdated
	writeJSON(w, 200, out)
}

// handleUpdatePanel downloads the latest release, replaces this binary and
// exits so systemd starts the new version. Binaries for the other
// architectures are cached in <data>/bin so nodes can update from the panel.
func (s *Server) handleUpdatePanel(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("INVOCATION_ID") == "" {
		fail(w, 400, "the panel is not running as a systemd service; update it by running the install command again")
		return
	}
	s.mon.mu.Lock()
	if s.mon.updating {
		s.mon.mu.Unlock()
		fail(w, 409, "an update is already running")
		return
	}
	s.mon.updating = true
	s.mon.mu.Unlock()
	defer func() { s.mon.mu.Lock(); s.mon.updating = false; s.mon.mu.Unlock() }()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	rel, err := update.Latest(ctx, s.updateRepo())
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	if !update.Newer(rel.Tag, proto.Version) {
		fail(w, 400, "already on the latest version "+proto.Version)
		return
	}
	self, err := update.SelfPath()
	if err != nil {
		logErr(w, err)
		return
	}
	// other architectures first (best effort), then our own binary
	binDir := filepath.Join(s.DataDir, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	for _, arch := range []string{"amd64", "arm64", "arm"} {
		if arch == runtime.GOARCH {
			continue
		}
		if u := rel.Assets[update.AssetName(arch)]; u != "" {
			dst := filepath.Join(binDir, "openv6-linux-"+arch)
			if tmp, err := update.Download(ctx, u, dst, false); err == nil {
				_ = os.Chmod(tmp, 0o755)
				_ = os.Rename(tmp, dst)
			}
		}
	}
	u := rel.Assets[update.AssetName(runtime.GOARCH)]
	if u == "" {
		fail(w, 502, "release has no binary for linux/"+runtime.GOARCH)
		return
	}
	tmp, err := update.Download(ctx, u, self, false)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	if err := update.Install(tmp, self, rel.Tag); err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.Store.AddEvent(0, "info", fmt.Sprintf("panel updated %s -> %s", proto.Version, rel.Tag))
	log.Printf("panel updated to %s, restarting", rel.Tag)
	writeJSON(w, 200, map[string]any{"ok": true, "version": rel.Tag})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(time.Second)
		os.Exit(0) // systemd restarts us with the new binary
	}()
}

// handleUpdateNodes asks remote nodes running another version to update
// from this panel's binary.
func (s *Server) handleUpdateNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.Store.ListNodes()
	if err != nil {
		logErr(w, err)
		return
	}
	only := pathID(r)
	pu := s.panelURL(r)
	var cmds []*commandState
	for _, n := range nodes {
		if n.IsLocal || (only > 0 && n.ID != only) {
			continue
		}
		var req proto.SyncRequest
		_ = json.Unmarshal([]byte(n.StateJSON), &req)
		if req.AgentVersion == proto.Version || req.Arch == "" {
			continue
		}
		cmds = append(cmds, s.queueCommand(n.ID, proto.Command{Type: "update", Version: proto.Version, URL: pu + "/download/" + req.Arch}))
	}
	writeJSON(w, 200, map[string]any{"commands": cmds})
}

// ---- backup ----

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	b, err := s.Store.Dump(proto.Version)
	if err != nil {
		logErr(w, err)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="openv6-backup-%s.json"`, time.Now().Format("20060102-1504")))
	writeJSON(w, 200, b)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var b store.Backup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&b); err != nil {
		fail(w, 400, "invalid backup file: "+err.Error())
		return
	}
	// keep a safety copy of the current state first
	if cur, err := s.Store.Dump(proto.Version); err == nil {
		_ = s.writeBackupFile(cur, "before-restore")
	}
	if err := s.Store.Restore(&b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.Store.AddEvent(0, "warn", "configuration restored from backup")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) backupDir() string { return filepath.Join(s.DataDir, "backups") }

func (s *Server) writeBackupFile(b *store.Backup, tag string) error {
	if err := os.MkdirAll(s.backupDir(), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(b, "", " ")
	name := fmt.Sprintf("openv6-%s-%s.json", tag, time.Now().Format("20060102-150405"))
	return os.WriteFile(filepath.Join(s.backupDir(), name), data, 0o600)
}

// autoBackup writes one backup per day and keeps the last 14 files.
func (s *Server) autoBackup() {
	s.mon.mu.Lock()
	due := time.Since(s.mon.lastBackup) > 24*time.Hour
	if due {
		s.mon.lastBackup = time.Now()
	}
	s.mon.mu.Unlock()
	if !due {
		return
	}
	b, err := s.Store.Dump(proto.Version)
	if err != nil {
		return
	}
	if err := s.writeBackupFile(b, "auto"); err != nil {
		log.Printf("auto backup: %v", err)
		return
	}
	files, _ := filepath.Glob(filepath.Join(s.backupDir(), "openv6-auto-*.json"))
	sort.Strings(files)
	for len(files) > 14 {
		_ = os.Remove(files[0])
		files = files[1:]
	}
}

// ---- logs ----

func (s *Server) nodeLog(id int64) *logbuf.Buffer {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	b := s.logs[id]
	if b == nil {
		b = logbuf.New(2000)
		s.logs[id] = b
	}
	return b
}

func (s *Server) handleNodeLogs(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	var lines []string
	if n.IsLocal && s.PanelLogs != nil {
		lines = s.PanelLogs.Tail(limit)
	} else {
		lines = s.nodeLog(n.ID).Tail(limit)
	}
	if q := strings.ToLower(r.URL.Query().Get("q")); q != "" {
		var f []string
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), q) {
				f = append(f, l)
			}
		}
		lines = f
	}
	if lines == nil {
		lines = []string{}
	}
	writeJSON(w, 200, map[string]any{"lines": lines})
}

func (s *Server) handleProxyChecks(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Store.ListChecks(pathID(r), 50)
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, cs)
}

func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	n := s.queueChecks(store.ProxyFilter{})
	writeJSON(w, 200, map[string]int{"queued": n})
}

// handleNodeRestart restarts a node's service. For the local node this
// restarts the whole panel process (systemd brings it back).
func (s *Server) handleNodeRestart(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	if !n.IsLocal {
		writeJSON(w, 200, s.queueCommand(n.ID, proto.Command{Type: "restart"}))
		return
	}
	if os.Getenv("INVOCATION_ID") == "" {
		fail(w, 400, "the panel is not running as a systemd service; restart it manually")
		return
	}
	s.Store.AddEvent(n.ID, "info", "panel restart requested")
	log.Printf("restart requested from the web UI")
	writeJSON(w, 200, map[string]bool{"ok": true, "panel": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() { time.Sleep(time.Second); os.Exit(0) }()
}

// handleNodeReapply removes and re-installs all addresses and routes on a node.
func (s *Server) handleNodeReapply(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.GetNode(pathID(r))
	if err != nil {
		fail(w, 404, "node not found")
		return
	}
	s.Store.AddEvent(n.ID, "info", fmt.Sprintf("node %q: network configuration re-applied", n.Name))
	writeJSON(w, 200, s.queueCommand(n.ID, proto.Command{Type: "reapply"}))
}
