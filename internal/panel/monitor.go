package panel

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"openv6/internal/proto"
	"openv6/internal/store"
	"openv6/internal/update"
)

// monitor holds background state of the panel: node online transitions,
// scheduled health checks, update checks and automatic backups.
type monitor struct {
	mu          sync.Mutex
	nodeOnline  map[int64]bool
	lastCheck   time.Time
	latest      *update.Release
	latestAt    time.Time
	latestErr   string
	notifiedVer string
	lastBackup  time.Time
	updating    bool
}

// RunBackground starts the panel's periodic jobs until ctx is done.
func (s *Server) RunBackground(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	go s.checkForUpdate(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.watchNodes()
		go s.checkLicense(ctx)
		s.noticeFreeLimit()
		s.scheduleHealthChecks()
		s.autoBackup()
		s.mon.mu.Lock()
		stale := time.Since(s.mon.latestAt) > 6*time.Hour
		s.mon.mu.Unlock()
		if stale {
			go s.checkForUpdate(ctx)
		}
	}
}

// watchNodes alerts when a node goes offline or comes back.
func (s *Server) watchNodes() {
	nodes, err := s.Store.ListNodes()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	s.mon.mu.Lock()
	defer s.mon.mu.Unlock()
	for _, n := range nodes {
		online := now-n.LastSeen <= onlineWindow*2 // tolerate a missed sync or two
		prev, known := s.mon.nodeOnline[n.ID]
		s.mon.nodeOnline[n.ID] = online
		if !known {
			continue // first observation after panel start: no alert
		}
		if prev && !online {
			s.Store.AddEvent(n.ID, "warn", fmt.Sprintf("node %q offline", n.Name))
			s.alert(AlertNodeOffline, s.msg("node_offline", n.Name))
		} else if !prev && online {
			s.alert(AlertNodeOffline, s.msg("node_online", n.Name))
		}
	}
}

func (s *Server) checkIntervalMinutes() int {
	v, err := strconv.Atoi(s.Store.GetSetting("check_interval_min", "10"))
	if err != nil || v < 0 {
		return 10
	}
	return v
}

// scheduleHealthChecks queues a test for every active proxy every N minutes.
func (s *Server) scheduleHealthChecks() {
	iv := s.checkIntervalMinutes()
	if iv == 0 {
		return
	}
	s.mon.mu.Lock()
	due := time.Since(s.mon.lastCheck) >= time.Duration(iv)*time.Minute
	if due {
		s.mon.lastCheck = time.Now()
	}
	s.mon.mu.Unlock()
	if !due {
		return
	}
	n := s.queueChecks(store.ProxyFilter{})
	if n > 0 {
		log.Printf("health check: testing %d proxies", n)
	}
}

// queueChecks queues test commands for active proxies on online nodes.
func (s *Server) queueChecks(f store.ProxyFilter) int {
	ps, err := s.Store.ListProxies(f)
	if err != nil {
		return 0
	}
	nodes, _ := s.Store.ListNodes()
	online := map[int64]bool{}
	for _, n := range nodes {
		online[n.ID] = time.Now().Unix()-n.LastSeen <= onlineWindow
	}
	now := time.Now().Unix()
	count := 0
	for _, p := range ps {
		if !p.Enabled || (p.ExpiresAt > 0 && p.ExpiresAt < now) || !online[p.NodeID] {
			continue
		}
		s.queueCommand(p.NodeID, proto.Command{Type: "test_proxy", ProxyID: p.ID})
		count++
	}
	return count
}

// onCheckResult stores a test result and alerts on state changes. A proxy
// is reported failing after 2 consecutive failures to avoid noise.
func (s *Server) onCheckResult(nodeID, proxyID int64, res proto.CommandResult) {
	rec := store.CheckRecord{At: time.Now().Unix(), OK: res.OK, IP: res.IP, Latency: res.Latency, Code: res.Code}
	if !res.OK {
		rec.Error = res.Output
	}
	before, after, err := s.Store.RecordCheck(proxyID, rec, res.Country, res.Org)
	if err != nil {
		return
	}
	p, err := s.Store.GetProxy(proxyID)
	if err != nil {
		return
	}
	name := proxyName(p)
	switch {
	case after == 2:
		s.Store.AddEvent(nodeID, "warn", fmt.Sprintf("proxy %s failing: %s", name, res.Output))
		s.alert(AlertProxyFail, s.msg("proxy_fail", name, res.Output))
	case before >= 2 && after == 0:
		s.Store.AddEvent(nodeID, "info", fmt.Sprintf("proxy %s works again", name))
		s.alert(AlertProxyFail, s.msg("proxy_ok", name, res.IP))
	}
}

func proxyName(p *store.Proxy) string {
	if p.Label != "" {
		return fmt.Sprintf("%q (:%d)", p.Label, p.Port)
	}
	return fmt.Sprintf(":%d", p.Port)
}

// ---- updates ----

// updateRepo is where updates come from. It is not a dashboard setting: an
// admin session (or a restored backup) must never be able to make the
// service download and run someone else's binary as root/SYSTEM. Whoever
// controls the machine can still point it elsewhere with OPENV6_UPDATE_REPO.
func (s *Server) updateRepo() string {
	if r := os.Getenv("OPENV6_UPDATE_REPO"); r != "" {
		return r
	}
	return "synthcoz/openv6-releases"
}

func (s *Server) checkForUpdate(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, s.updateRepo())
	s.mon.mu.Lock()
	s.mon.latestAt = time.Now()
	if err != nil {
		s.mon.latestErr = err.Error()
		s.mon.mu.Unlock()
		return
	}
	s.mon.latest, s.mon.latestErr = rel, ""
	notify := update.Newer(rel.Tag, proto.Version) && s.mon.notifiedVer != rel.Tag
	if notify {
		s.mon.notifiedVer = rel.Tag
	}
	s.mon.mu.Unlock()
	if notify {
		s.Store.AddEvent(0, "info", "new version available: "+rel.Tag)
		s.alert(AlertUpdate, s.msg("update", rel.Tag, proto.Version))
	}
}
