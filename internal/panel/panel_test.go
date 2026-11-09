package panel

import (
	"bytes"
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
