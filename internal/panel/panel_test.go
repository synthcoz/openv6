package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"openv6/internal/license"
	"openv6/internal/proto"
	"openv6/internal/store"
)

type env struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	cl  *http.Client
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DB.Close() }) // Windows cannot delete an open file
	srv := New(st, fstest.MapFS{"index.html": {Data: []byte("ok")}}, dir, 8080, "")
	srv.ForcePro = true // feature tests; licensing has its own test
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, srv: srv, ts: ts, cl: &http.Client{Jar: jar}}
	e.call("POST", "/api/setup", map[string]string{"username": "admin", "password": "password123"}, nil, 200)
	return e
}

func (e *env) call(method, path string, body, out any, want int) {
	e.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	res, err := e.cl.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		e.t.Fatalf("%s %s: status %d want %d (%v)", method, path, res.StatusCode, want, m)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) sync(token string, req proto.SyncRequest) proto.SyncResponse {
	e.t.Helper()
	b, _ := json.Marshal(req)
	hr, _ := http.NewRequest("POST", e.ts.URL+"/api/agent/sync", bytes.NewReader(b))
	hr.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(hr)
	if err != nil || res.StatusCode != 200 {
		e.t.Fatalf("sync: %v %v", err, res)
	}
	defer res.Body.Close()
	var out proto.SyncResponse
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out
}

func homeNet(prefix string) proto.NetState {
	return proto.NetState{DefaultIface: "eth0", HasV6Route: true, LANv4: []string{"192.168.1.10"},
		Interfaces: []proto.Interface{{Name: "eth0", Prefixes: []string{prefix}}}}
}

func TestEndToEndFlow(t *testing.T) {
	e := newEnv(t)
	// unauthenticated API access is refused
	res, _ := http.Get(e.ts.URL + "/api/nodes")
	if res.StatusCode != 401 {
		t.Fatalf("want 401, got %d", res.StatusCode)
	}

	var node struct{ ID int64 }
	e.call("POST", "/api/nodes", map[string]string{"name": "home"}, &node, 200)
	var inst struct{ Token string }
	e.call("GET", fmt.Sprintf("/api/nodes/%d/install", node.ID), nil, &inst, 200)

	// first sync teaches the panel the /64
	e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	var pools []store.Pool
	e.call("GET", fmt.Sprintf("/api/pools?node_id=%d", node.ID), nil, &pools, 200)
	if len(pools) != 1 || pools[0].CIDR != "2405:4802:1:2::/64" {
		t.Fatalf("auto pool %+v", pools)
	}

	var created struct{ Created int }
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": node.ID, "pool_id": pools[0].ID, "count": 3,
		"auth": "random", "label_prefix": "FB"}, &created, 200)
	if created.Created != 3 {
		t.Fatal("bulk create")
	}
	resp := e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	if len(resp.Proxies) != 3 || !strings.HasPrefix(resp.Proxies[0].IP, "2405:4802:1:2:") || resp.Proxies[0].User == "" {
		t.Fatalf("desired state %+v", resp.Proxies)
	}

	// ISP changes the prefix: same ports, same suffix, new prefix
	oldIP := resp.Proxies[0].IP
	resp = e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:9:9::/64")})
	if !strings.HasPrefix(resp.Proxies[0].IP, "2405:4802:9:9:") || resp.Proxies[0].IP[len(resp.Proxies[0].IP)-6:] != oldIP[len(oldIP)-6:] {
		t.Fatalf("prefix change: %s -> %s", oldIP, resp.Proxies[0].IP)
	}

	// export with labels in the anti-detect formats
	var list []struct {
		ID    int64
		Label string
		Port  int
	}
	e.call("GET", "/api/proxies", nil, &list, 200)
	if list[0].Label != "FB 1" {
		t.Fatalf("label %q", list[0].Label)
	}
	ids := []int64{list[0].ID}
	for format, want := range map[string]string{
		"host:port:user:pass":          "192.168.1.10:",
		"host:port@user:pass":          "@",
		"scheme://host:port:user:pass": "socks5://192.168.1.10:",
	} {
		var x struct{ Text string }
		e.call("POST", "/api/proxies/export", map[string]any{"ids": ids, "format": format, "scheme": "socks5"}, &x, 200)
		if !strings.Contains(x.Text, want) {
			t.Fatalf("export %s: %q", format, x.Text)
		}
	}

	// health check results: two failures -> failing, then recovery
	e.call("POST", "/api/proxies/action", map[string]any{"ids": ids, "action": "test"}, nil, 200)
	resp = e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:9:9::/64")})
	if len(resp.Commands) != 1 || resp.Commands[0].Type != "test_proxy" {
		t.Fatalf("commands %+v", resp.Commands)
	}
	fail := proto.CommandResult{ID: resp.Commands[0].ID, OK: false, Output: "timeout"}
	e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:9:9::/64"), Results: []proto.CommandResult{fail}})
	p, _ := e.srv.Store.GetProxy(ids[0])
	if p.CheckOK != 0 || p.FailStreak != 1 {
		t.Fatalf("after failure %+v", p)
	}
	e.srv.onCheckResult(node.ID, ids[0], proto.CommandResult{OK: true, IP: "2405::1", Latency: 120, Country: "VN", Org: "AS18403 FPT"})
	p, _ = e.srv.Store.GetProxy(ids[0])
	if p.CheckOK != 1 || p.CheckCountry != "VN" || p.CheckLatency != 120 || p.FailStreak != 0 {
		t.Fatalf("after success %+v", p)
	}
	var hist []store.CheckRecord
	e.call("GET", fmt.Sprintf("/api/proxies/%d/checks", ids[0]), nil, &hist, 200)
	if len(hist) != 2 || !hist[0].OK {
		t.Fatalf("history %+v", hist)
	}

	// agent logs are kept per node
	e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:9:9::/64"), Logs: []string{"hello from agent"}})
	var logs struct{ Lines []string }
	e.call("GET", fmt.Sprintf("/api/nodes/%d/logs", node.ID), nil, &logs, 200)
	if len(logs.Lines) != 1 || logs.Lines[0] != "hello from agent" {
		t.Fatalf("logs %+v", logs)
	}
}

func TestBackupRestore(t *testing.T) {
	e := newEnv(t)
	var node struct{ ID int64 }
	e.call("POST", "/api/nodes", map[string]string{"name": "vps"}, &node, 200)
	var pool store.Pool
	e.call("POST", "/api/pools", map[string]any{"node_id": node.ID, "cidr": "2a01:4f8:1::/48", "mode": "anyip"}, &pool, 200)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": node.ID, "pool_id": pool.ID, "count": 5, "auth": "random", "spread": true}, nil, 200)

	b, err := e.srv.Store.Dump(proto.Version)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	before, _ := e.srv.Store.ListProxies(store.ProxyFilter{})

	e.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{before[0].ID, before[1].ID}, "action": "delete"}, nil, 200)

	var back store.Backup
	_ = json.Unmarshal(raw, &back)
	if err := e.srv.Store.Restore(&back); err != nil {
		t.Fatal(err)
	}
	after, _ := e.srv.Store.ListProxies(store.ProxyFilter{})
	if len(after) != 5 || after[0].Suffix != before[0].Suffix || after[0].AuthPass != before[0].AuthPass || after[0].Subnet != before[0].Subnet {
		t.Fatalf("restore mismatch: %d proxies", len(after))
	}
	if e.srv.Store.UserCount() != 1 {
		t.Fatal("users not restored")
	}
}

func TestSettingsHideTelegramToken(t *testing.T) {
	e := newEnv(t)
	var out map[string]any
	e.call("PUT", "/api/settings", map[string]string{"telegram_token": "123:secret", "telegram_chat": "42", "check_interval_min": "5"}, &out, 200)
	if out["telegram_token_set"] != true || out["telegram_chat"] != "42" || out["check_interval_min"] != "5" {
		t.Fatalf("settings %+v", out)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "secret") {
		t.Fatal("token leaked to the browser")
	}
	e.call("PUT", "/api/settings", map[string]string{"check_interval_min": "-1"}, nil, 400)
}

func TestWatchNodesAlertsOnce(t *testing.T) {
	e := newEnv(t)
	n, _ := e.srv.Store.CreateNode("n1", "", false)
	_ = e.srv.Store.TouchNode(n.ID, "{}")
	e.srv.watchNodes() // first observation: online
	_, _ = e.srv.Store.DB.Exec(`UPDATE nodes SET last_seen=? WHERE id=?`, time.Now().Unix()-600, n.ID)
	e.srv.watchNodes()
	evs, _ := e.srv.Store.ListEvents(10)
	found := 0
	for _, ev := range evs {
		if strings.Contains(ev.Message, "offline") {
			found++
		}
	}
	e.srv.watchNodes()
	evs2, _ := e.srv.Store.ListEvents(10)
	if found != 1 || len(evs2) != len(evs) {
		t.Fatalf("offline events: %d (then %d events)", found, len(evs2))
	}
}

func TestScopeBypassDNS(t *testing.T) {
	e := newEnv(t)
	var node struct{ ID int64 }
	e.call("POST", "/api/nodes", map[string]string{"name": "home"}, &node, 200)
	var inst struct{ Token string }
	e.call("GET", fmt.Sprintf("/api/nodes/%d/install", node.ID), nil, &inst, 200)
	// WARP owns the default route; the panel must use the primary NIC's /64
	net := homeNet("2405:4802:1:2::/64")
	net.DefaultIface, net.PrimaryIface = "CloudflareWARP", "eth0"
	e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	var pools []store.Pool
	e.call("GET", fmt.Sprintf("/api/pools?node_id=%d", node.ID), nil, &pools, 200)
	if pools[0].CIDR != "2405:4802:1:2::/64" {
		t.Fatalf("pool %+v", pools[0])
	}
	// Internet sharing without a password is refused with a clear message
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": node.ID, "pool_id": pools[0].ID, "count": 1, "auth": "none", "scope": "any"}, nil, 400)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": node.ID, "pool_id": pools[0].ID, "count": 1, "auth": "random", "scope": "any"}, nil, 200)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": node.ID, "pool_id": pools[0].ID, "count": 1, "auth": "none", "scope": "local"}, nil, 200)
	var list []struct {
		ID    int64
		Scope string
		Host  string
	}
	e.call("GET", "/api/proxies", nil, &list, 200)
	if list[0].Scope != "any" || list[1].Scope != "local" || list[1].Host != "127.0.0.1" {
		t.Fatalf("scopes %+v", list)
	}
	e.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{list[0].ID}, "action": "remove_auth"}, nil, 400)

	e.call("PUT", fmt.Sprintf("/api/nodes/%d", node.ID), map[string]any{"bypass": false, "dns": "cloudflare"}, nil, 200)
	e.call("PUT", fmt.Sprintf("/api/nodes/%d", node.ID), map[string]any{"dns": "not-an-ip"}, nil, 400)
	resp := e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	if resp.Bypass || len(resp.DNS) != 3 || resp.Proxies[0].Scope != "any" {
		t.Fatalf("desired %+v", resp)
	}
	e.call("PUT", fmt.Sprintf("/api/nodes/%d", node.ID), map[string]any{"bypass": true, "dns": "2001:4860:4860::8888, 8.8.8.8"}, nil, 200)
	resp = e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	if !resp.Bypass || len(resp.DNS) != 2 {
		t.Fatalf("desired %+v", resp)
	}
}

func TestMigrationScopeForExistingProxies(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.Open(filepath.Join(dir, "m.db"))
	t.Cleanup(func() { st.DB.Close() })
	n, _ := st.CreateNode("n", "", false)
	pools, _ := st.ListPools(n.ID)
	_ = st.CreateProxies([]*store.Proxy{{NodeID: n.ID, PoolID: pools[0].ID, Port: 10001, Protocol: "both", Suffix: "0000000000abcdef", AuthUser: "u", AuthPass: "p", Enabled: true}})
	// simulate a v0.2 database: undo migration 3 marker and re-run it
	_, _ = st.DB.Exec(`UPDATE proxies SET scope='lan'`)
	_, _ = st.DB.Exec(`UPDATE proxies SET scope='any' WHERE auth_user <> ''`)
	ps, _ := st.ListProxies(store.ProxyFilter{})
	if ps[0].Scope != "any" {
		t.Fatalf("password proxies must stay reachable from the Internet after upgrade, got %q", ps[0].Scope)
	}
}

func TestRelayLimitsAndAPIToken(t *testing.T) {
	e := newEnv(t)
	mk := func(name string) (int64, string) {
		var n struct{ ID int64 }
		e.call("POST", "/api/nodes", map[string]string{"name": name}, &n, 200)
		var inst struct{ Token string }
		e.call("GET", fmt.Sprintf("/api/nodes/%d/install", n.ID), nil, &inst, 200)
		return n.ID, inst.Token
	}
	homeID, homeTok := mk("home")
	vpsID, vpsTok := mk("vps")
	e.sync(homeTok, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	vpsNet := homeNet("2001:db8:5::/64")
	vpsNet.PublicV4 = "203.0.113.7"
	e.sync(vpsTok, proto.SyncRequest{AgentVersion: proto.Version, Net: vpsNet})
	var pools []store.Pool
	e.call("GET", fmt.Sprintf("/api/pools?node_id=%d", homeID), nil, &pools, 200)

	// relay requires the VPS to be a relay first
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": homeID, "pool_id": pools[0].ID, "count": 2,
		"auth": "random", "relay_node_id": vpsID}, nil, 400)
	e.call("PUT", fmt.Sprintf("/api/nodes/%d", vpsID), map[string]any{"relay_port": 7443, "upnp": false}, nil, 200)
	// without a password: refused
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": homeID, "pool_id": pools[0].ID, "count": 2,
		"auth": "none", "relay_node_id": vpsID}, nil, 400)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": homeID, "pool_id": pools[0].ID, "count": 2,
		"auth": "random", "relay_node_id": vpsID, "max_conns": 50, "rate_kbps": 2048}, nil, 200)

	// the VPS gets the relay server spec, the home node waits for its fingerprint
	vr := e.sync(vpsTok, proto.SyncRequest{AgentVersion: proto.Version, Net: vpsNet, RelayFP: strings.Repeat("ab", 32)})
	if vr.RelayServer == nil || vr.RelayServer.Port != 7443 || len(vr.RelayServer.Clients) != 1 || len(vr.RelayServer.Clients[0].Maps) != 2 {
		t.Fatalf("relay server spec %+v", vr.RelayServer)
	}
	hr := e.sync(homeTok, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	if len(hr.RelayClients) != 1 || hr.RelayClients[0].Addr != "203.0.113.7:7443" || hr.RelayClients[0].Token != vr.RelayServer.Clients[0].Token ||
		len(hr.RelayClients[0].Ports) != 2 || hr.RelayClients[0].Fingerprint != strings.Repeat("ab", 32) {
		t.Fatalf("relay client spec %+v", hr.RelayClients)
	}
	if hr.Proxies[0].MaxConns != 50 || hr.Proxies[0].RateKBps != 2048 {
		t.Fatalf("limits %+v", hr.Proxies[0])
	}
	// export through the relay
	var list []struct {
		ID        int64
		Port      int
		RelayPort int `json:"relay_port"`
	}
	e.call("GET", "/api/proxies", nil, &list, 200)
	var x struct{ Text string }
	e.call("POST", "/api/proxies/export", map[string]any{"ids": []int64{list[0].ID}, "via": "relay"}, &x, 200)
	if !strings.HasPrefix(x.Text, fmt.Sprintf("203.0.113.7:%d:", list[0].RelayPort)) {
		t.Fatalf("relay export %q", x.Text)
	}
	// relay port of the VPS cannot collide with relayed ports
	e.call("PUT", fmt.Sprintf("/api/nodes/%d", vpsID), map[string]any{"relay_port": list[0].RelayPort}, nil, 400)
	// turning relay off for the selection
	e.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{list[0].ID, list[1].ID}, "action": "set_relay", "value": "0"}, nil, 200)
	vr = e.sync(vpsTok, proto.SyncRequest{AgentVersion: proto.Version, Net: vpsNet, RelayFP: strings.Repeat("ab", 32)})
	if len(vr.RelayServer.Clients) != 0 {
		t.Fatalf("relay clients left %+v", vr.RelayServer)
	}

	// API tokens
	var tok struct{ Token string }
	e.call("POST", "/api/tokens", map[string]string{"name": "script"}, &tok, 200)
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/proxies", nil)
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("api token: %v %v", err, res.StatusCode)
	}
	req, _ = http.NewRequest("POST", e.ts.URL+"/api/tokens", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("Content-Type", "application/json")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 403 {
		t.Fatalf("token creating tokens: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", e.ts.URL+"/api/proxies", nil)
	req.Header.Set("Authorization", "Bearer ov6_wrong")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Fatalf("bad token: %d", res.StatusCode)
	}
}

func TestStaticGzipETag(t *testing.T) {
	big := strings.Repeat("openv6 dashboard ", 200)
	h := &staticHandler{fsys: fstest.MapFS{"index.html": {Data: []byte(big)}, "app.js": {Data: []byte("x")}}}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(big) {
		t.Fatalf("gzip: %d %q %d", rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	etag := rec.Header().Get("ETag")
	req = httptest.NewRequest("GET", "/index.html", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 304 {
		t.Fatalf("etag: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app.js", nil))
	if !strings.Contains(rec.Header().Get("Content-Type"), "javascript") || rec.Body.String() != "x" {
		t.Fatalf("app.js %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/../secret", nil))
	if rec.Code != 404 {
		t.Fatalf("traversal %d", rec.Code)
	}
}

func TestTrayShutdown(t *testing.T) {
	e := newEnv(t)
	e.srv.TrayToken = "tt"
	called := make(chan bool, 1)
	e.srv.OnShutdown = func() { called <- true }
	if _, err := e.srv.Store.CreateNode("local", "", true); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/tray/shutdown", nil)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 403 {
		t.Fatalf("without token: %d", res.StatusCode)
	}
	req.Header.Set("X-Tray-Token", "tt")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 200 {
		t.Fatalf("with token: %d", res.StatusCode)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("OnShutdown not called")
	}
}

func TestRemoteSetupNeedsCode(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DB.Close() })
	srv := New(st, fstest.MapFS{"index.html": {Data: []byte("ok")}}, dir, 8080, "")
	code := srv.SetupCode()
	h := srv.Handler()
	try := func(remote, host, code string) int {
		b, _ := json.Marshal(map[string]string{"username": "admin", "password": "password123", "code": code})
		req := httptest.NewRequest("POST", "/api/setup", bytes.NewReader(b))
		req.RemoteAddr, req.Host = remote, host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := try("203.0.113.9:5555", "203.0.113.1:8080", ""); c != 403 {
		t.Fatalf("remote without code: %d", c)
	}
	if c := try("127.0.0.1:5555", "evil.example.com:8080", "000"); c != 403 { // DNS rebinding
		t.Fatalf("rebinding: %d", c)
	}
	if c := try("203.0.113.9:5555", "203.0.113.1:8080", code); c != 200 {
		t.Fatalf("remote with code: %d", c)
	}
	if srv.SetupCode() != "" {
		t.Fatal("code kept after setup")
	}
}

func TestLicenseFreeTierAndActivation(t *testing.T) {
	// fake Polar
	var validateStatus = 200
	polar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		key, _ := b["key"].(string)
		benefit := "e2f37fe8-4ecd-430d-a02e-8b8265487fa8"
		if key == "OTHER-PRODUCT" {
			benefit = "00000000-0000-0000-0000-000000000000"
		}
		lk := map[string]any{"benefit_id": benefit, "display_key": "****-1234", "status": "granted", "customer": map[string]any{"email": "a@b.c"}}
		switch r.URL.Path {
		case "/v1/customer-portal/license-keys/activate":
			if key == "BAD" {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":"ResourceNotFound","detail":"not found"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "act-1", "license_key": lk})
		case "/v1/customer-portal/license-keys/validate":
			w.WriteHeader(validateStatus)
			if validateStatus != 200 {
				fmt.Fprint(w, `{"error":"ResourceNotFound","detail":"License key not found"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(lk)
		case "/v1/customer-portal/license-keys/deactivate":
			w.WriteHeader(204)
		}
	}))
	defer polar.Close()
	old := license.APIBase
	license.APIBase = polar.URL
	defer func() { license.APIBase = old }()

	e := newEnv(t)
	e.srv.ForcePro = false
	local, _ := e.srv.Store.CreateNode("local", "", true)
	tok := local.Token
	e.sync(tok, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	var pools []store.Pool
	e.call("GET", fmt.Sprintf("/api/pools?node_id=%d", local.ID), nil, &pools, 200)

	// free: 3 proxies, 1 machine, no Pro features
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": local.ID, "pool_id": pools[0].ID, "count": 4, "auth": "random"}, nil, 402)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": local.ID, "pool_id": pools[0].ID, "count": 3, "auth": "random"}, nil, 200)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": local.ID, "pool_id": pools[0].ID, "count": 1, "auth": "random"}, nil, 402)
	e.call("POST", "/api/nodes", map[string]string{"name": "vps"}, nil, 402)
	e.call("POST", "/api/tokens", map[string]string{"name": "x"}, nil, 402)
	e.call("PUT", fmt.Sprintf("/api/nodes/%d", local.ID), map[string]any{"upnp": true}, nil, 402)

	// wrong key / other product
	e.call("POST", "/api/license", map[string]string{"key": "BAD"}, nil, 400)
	e.call("POST", "/api/license", map[string]string{"key": "OTHER-PRODUCT"}, nil, 400)
	// activation unlocks Pro
	var lv struct{ Pro bool }
	e.call("POST", "/api/license", map[string]string{"key": "OV6-GOOD"}, &lv, 200)
	if !lv.Pro {
		t.Fatal("not pro after activation")
	}
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": local.ID, "pool_id": pools[0].ID, "count": 5, "auth": "random"}, nil, 200)
	resp := e.sync(tok, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	if len(resp.Proxies) != 8 {
		t.Fatalf("pro serves %d proxies", len(resp.Proxies))
	}
	// Polar unreachable: stays Pro (grace); revoked: back to free, only 3 proxies served
	st := e.srv.licState()
	st.CheckedAt = 0
	e.srv.saveLic(st)
	license.APIBase = "http://127.0.0.1:1"
	e.srv.checkLicense(context.Background())
	if !e.srv.pro() {
		t.Fatal("lost Pro on network error")
	}
	license.APIBase = polar.URL
	validateStatus = 404
	st = e.srv.licState()
	st.CheckedAt = 0
	e.srv.saveLic(st)
	e.srv.checkLicense(context.Background())
	if e.srv.pro() {
		t.Fatal("revoked key still Pro")
	}
	resp = e.sync(tok, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	if len(resp.Proxies) != 3 {
		t.Fatalf("free serves %d proxies", len(resp.Proxies))
	}
	e.call("DELETE", "/api/license", nil, &lv, 200)
}

func TestCustomerAccounts(t *testing.T) {
	e := newEnv(t)
	local, _ := e.srv.Store.CreateNode("local", "", true)
	e.sync(local.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: homeNet("2405:4802:1:2::/64")})
	var pools []store.Pool
	e.call("GET", fmt.Sprintf("/api/pools?node_id=%d", local.ID), nil, &pools, 200)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": local.ID, "pool_id": pools[0].ID, "count": 3, "auth": "random"}, nil, 200)
	e.call("POST", "/api/users", map[string]string{"username": "khach1", "password": "password123", "role": "customer"}, nil, 200)
	var users []store.UserInfo
	e.call("GET", "/api/users", nil, &users, 200)
	var cid int64
	for _, u := range users {
		if u.Username == "khach1" {
			cid = u.ID
		}
	}
	var list []struct{ ID int64 }
	e.call("GET", "/api/proxies", nil, &list, 200)
	e.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{list[0].ID, list[1].ID}, "action": "set_owner", "value": fmt.Sprint(cid)}, nil, 200)

	jar, _ := cookiejar.New(nil)
	c := &env{t: t, srv: e.srv, ts: e.ts, cl: &http.Client{Jar: jar}}
	c.call("POST", "/api/login", map[string]string{"username": "khach1", "password": "password123"}, nil, 200)
	var mine []struct{ ID int64 }
	c.call("GET", "/api/proxies", nil, &mine, 200)
	if len(mine) != 2 {
		t.Fatalf("customer sees %d proxies", len(mine))
	}
	c.call("GET", "/api/nodes", nil, nil, 403)
	c.call("GET", "/api/dashboard", nil, nil, 403)
	c.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{list[2].ID}, "action": "regen_pass"}, nil, 403) // not theirs
	c.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{mine[0].ID}, "action": "delete"}, nil, 403)
	c.call("POST", "/api/proxies/action", map[string]any{"ids": []int64{mine[0].ID}, "action": "regen_pass"}, nil, 200)
	var x struct{ Text string }
	c.call("POST", "/api/proxies/export", map[string]any{"ids": []int64{list[0].ID, list[1].ID, list[2].ID}}, &x, 200)
	if strings.Count(x.Text, "\n") != 2 {
		t.Fatalf("customer export: %q", x.Text)
	}
	e.call("DELETE", fmt.Sprintf("/api/users/%d", cid), nil, nil, 200)
	p, _ := e.srv.Store.GetProxy(list[0].ID)
	if p.OwnerID != 0 {
		t.Fatal("owner kept after account deletion")
	}
}

func TestBlockedSitesAndSiteTest(t *testing.T) {
	e := newEnv(t)
	var node struct{ ID int64 }
	e.call("POST", "/api/nodes", map[string]string{"name": "home"}, &node, 200)
	var inst struct{ Token string }
	e.call("GET", fmt.Sprintf("/api/nodes/%d/install", node.ID), nil, &inst, 200)
	net := homeNet("2405:4802:1:2::/64")
	e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	var pools []store.Pool
	e.call("GET", fmt.Sprintf("/api/pools?node_id=%d", node.ID), nil, &pools, 200)
	e.call("POST", "/api/proxies/bulk", map[string]any{"node_id": node.ID, "pool_id": pools[0].ID, "count": 1, "auth": "random"}, nil, 200)

	// defaults: nothing blocked, encrypted DNS fallback on
	resp := e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	if len(resp.Block) != 0 || !resp.SecureDNS {
		t.Fatalf("defaults %+v %v", resp.Block, resp.SecureDNS)
	}
	// unreadable entries are refused with their text
	e.call("PUT", "/api/settings", map[string]string{"block_sites": "facebook.com\nnot a site"}, nil, 400)
	var st map[string]any
	e.call("PUT", "/api/settings", map[string]string{"block_sites": "https://www.Facebook.com/login\n# work\n*.ads.example.com, 203.0.113.0/24\nfacebook.com\nwww.facebook.com", "secure_dns": "0"}, &st, 200)
	if st["block_sites"] != "www.facebook.com\n*.ads.example.com\n203.0.113.0/24\nfacebook.com" {
		t.Fatalf("saved list %q", st["block_sites"])
	}
	resp = e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	if len(resp.Block) != 4 || resp.SecureDNS {
		t.Fatalf("desired %+v %v", resp.Block, resp.SecureDNS)
	}

	// site test: queued for the proxy's node with the cleaned host name
	var list []struct{ ID int64 }
	e.call("GET", "/api/proxies", nil, &list, 200)
	e.call("POST", fmt.Sprintf("/api/proxies/%d/test-site", list[0].ID), map[string]string{"host": "not a site"}, nil, 400)
	var cmd struct{ ID, Type string }
	e.call("POST", fmt.Sprintf("/api/proxies/%d/test-site", list[0].ID), map[string]string{"host": "https://Example.com/x"}, &cmd, 200)
	if cmd.Type != "test_site" || cmd.ID == "" {
		t.Fatalf("command %+v", cmd)
	}
	resp = e.sync(inst.Token, proto.SyncRequest{AgentVersion: proto.Version, Net: net})
	found := false
	for _, c := range resp.Commands {
		if c.ID == cmd.ID && c.Type == "test_site" && c.Host == "example.com" && c.ProxyID == list[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("command not delivered: %+v", resp.Commands)
	}
	e.call("POST", "/api/proxies/999/test-site", map[string]string{"host": "example.com"}, nil, 404)
}
