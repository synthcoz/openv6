// openv6: create and manage static IPv6 proxies (HTTP + SOCKS5) from the
// IPv6 prefix of a home network, VPS or homelab, with a central web panel.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"openv6/internal/agent"
	"openv6/internal/logbuf"
	"openv6/internal/netcfg"
	"openv6/internal/panel"
	"openv6/internal/proto"
	"openv6/internal/store"
)

//go:embed web
var webFS embed.FS

const usage = `openv6 %s - static IPv6 proxies with a web panel

Usage:
  openv6 server  [-listen :8080] [-data /var/lib/openv6] [-no-local-agent]
                  [-tls-cert FILE -tls-key FILE]
      Run the web panel (and, by default, an agent for this machine).

  openv6 agent   -panel URL -token TOKEN [-data /var/lib/openv6-agent]
      Run a node that is managed by a remote panel.

  openv6 install server|agent [same flags as above]
      Install as a systemd service (run as root).

  openv6 uninstall
      Stop services and remove every IPv6 address/route openv6 added.

  openv6 reset-password [-data DIR] [-user NAME]
      Print a new random password for a panel user.

  openv6 selftest
      Show this machine's IPv6/VPN situation and test VPN-bypass routing in an
      isolated network namespace (the real network is not touched).

  openv6 version
`

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) < 2 {
		fmt.Printf(usage, proto.Version)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(os.Args[2:])
	case "agent":
		err = runAgent(os.Args[2:])
	case "install":
		err = runInstall(os.Args[2:])
	case "uninstall":
		err = runUninstall()
	case "reset-password":
		err = runResetPassword(os.Args[2:])
	case "selftest":
		err = runSelfTest()
	case "version", "-v", "--version":
		fmt.Println(proto.Version)
	default:
		fmt.Printf(usage, proto.Version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func signalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-ch; cancel() }()
	return ctx
}

func runServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "panel listen address")
	data := fs.String("data", "/var/lib/openv6", "data directory")
	noLocal := fs.Bool("no-local-agent", false, "do not run proxies on this machine (panel only)")
	dry := fs.Bool("dry-run", false, "development: do not change kernel networking")
	cert := fs.String("tls-cert", "", "TLS certificate file (enables HTTPS)")
	key := fs.String("tls-key", "", "TLS key file")
	_ = fs.Parse(args)

	if err := os.MkdirAll(*data, 0o700); err != nil {
		return err
	}
	logs := logbuf.New(5000)
	log.SetOutput(io.MultiWriter(os.Stderr, logs))
	st, err := store.Open(filepath.Join(*data, "openv6.db"))
	if err != nil {
		return err
	}
	_, portStr, err := net.SplitHostPort(*listen)
	if err != nil {
		return fmt.Errorf("bad -listen: %w", err)
	}
	port, _ := strconv.Atoi(portStr)

	web, _ := fs_sub(webFS, "web")
	exe, _ := os.Executable()
	srv := panel.New(st, web, *data, port, exe)
	srv.PanelLogs = logs
	ctx := signalContext()
	go srv.RunBackground(ctx)

	if !*noLocal {
		local, err := st.LocalNode()
		if err == store.ErrNotFound {
			host, _ := os.Hostname()
			local, err = st.CreateNode("local ("+host+")", "", true)
		}
		if err != nil {
			return err
		}
		scheme := "http"
		if *cert != "" {
			scheme = "https"
		}
		ag := agent.New(agent.Config{PanelURL: fmt.Sprintf("%s://127.0.0.1:%d", scheme, port), Token: local.Token,
			DataDir: filepath.Join(*data, "agent"), DryRun: *dry, InsecureTLS: *cert != ""})
		go func() {
			time.Sleep(500 * time.Millisecond)
			ag.Run(ctx)
		}()
	}

	hs := &http.Server{Addr: *listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = hs.Shutdown(sctx)
	}()
	log.Printf("panel listening on %s (data %s)", *listen, *data)
	if *cert != "" {
		err = hs.ListenAndServeTLS(*cert, *key)
	} else {
		err = hs.ListenAndServe()
	}
	if err == http.ErrServerClosed {
		time.Sleep(300 * time.Millisecond) // let the local agent close listeners
		return nil
	}
	return err
}

func fs_sub(f embed.FS, dir string) (fs.FS, error) { return fs.Sub(f, dir) }

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	panelURL := fs.String("panel", "", "panel URL, e.g. https://panel.example.com:8080")
	token := fs.String("token", "", "node token (from the panel)")
	data := fs.String("data", "/var/lib/openv6-agent", "data directory")
	dry := fs.Bool("dry-run", false, "development: do not change kernel networking")
	insecure := fs.Bool("insecure-tls", false, "skip TLS certificate verification (self-signed panel)")
	_ = fs.Parse(args)
	if *panelURL == "" || *token == "" {
		return fmt.Errorf("-panel and -token are required")
	}
	logs := logbuf.New(2000)
	log.SetOutput(io.MultiWriter(os.Stderr, logs))
	ag := agent.New(agent.Config{PanelURL: *panelURL, Token: *token, DataDir: *data, DryRun: *dry, InsecureTLS: *insecure,
		Logs: logs, AllowUpdate: true})
	ag.Run(signalContext())
	return nil
}

const unitTmpl = `[Unit]
Description=openv6 %s
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/openv6 %s
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`

func runInstall(args []string) error {
	if len(args) < 1 || (args[0] != "server" && args[0] != "agent") {
		return fmt.Errorf("usage: openv6 install server|agent [flags]")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root")
	}
	mode := args[0]
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe != "/usr/local/bin/openv6" {
		b, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		if err := os.WriteFile("/usr/local/bin/openv6.new", b, 0o755); err != nil {
			return err
		}
		if err := os.Rename("/usr/local/bin/openv6.new", "/usr/local/bin/openv6"); err != nil {
			return err
		}
	}
	var quoted []string
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	name := "openv6-" + mode
	unit := fmt.Sprintf(unitTmpl, mode, strings.Join(quoted, " "))
	if err := os.WriteFile("/etc/systemd/system/"+name+".service", []byte(unit), 0o600); err != nil {
		return err
	}
	// kernel limits for many addresses / connections, persistent across reboots
	if err := os.WriteFile("/etc/sysctl.d/99-openv6.conf", []byte(netcfg.SysctlConf()), 0o644); err == nil {
		_ = exec.Command("sysctl", "-q", "-p", "/etc/sysctl.d/99-openv6.conf").Run()
	}
	// if ufw is active, let the LAN reach the panel (proxy ports are opened by the agent)
	if mode == "server" && netcfg.UFWActive() {
		port := "8080"
		for i, a := range args {
			if (a == "-listen" || a == "--listen") && i+1 < len(args) {
				if _, p, err := net.SplitHostPort(args[i+1]); err == nil {
					port = p
				}
			}
		}
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			_ = exec.Command("ufw", "allow", "from", cidr, "to", "any", "port", port, "proto", "tcp", "comment", "openv6 panel").Run()
		}
		fmt.Println("ufw is active: opened the panel port " + port + " for LAN addresses")
	}
	for _, c := range [][]string{{"daemon-reload"}, {"enable", name}, {"restart", name}} {
		if out, err := exec.Command("systemctl", c...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v %s", strings.Join(c, " "), err, out)
		}
	}
	fmt.Printf("installed and started %s.service\n", name)
	if mode == "server" {
		fmt.Println("open http://<this-machine-ip>:8080 in a browser to create the admin account")
	}
	fmt.Println("logs: journalctl -u " + name + " -f")
	return nil
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$`;&|<>()") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s) + `"`
}

func runUninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root")
	}
	for _, n := range []string{"openv6-server", "openv6-agent"} {
		_ = exec.Command("systemctl", "disable", "--now", n).Run()
		_ = os.Remove("/etc/systemd/system/" + n + ".service")
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	for _, d := range []string{"/var/lib/openv6/agent", "/var/lib/openv6-agent"} {
		if _, err := os.Stat(filepath.Join(d, "netstate.json")); err == nil {
			netcfg.NewManager(d).Cleanup()
			fmt.Println("removed IPv6 addresses installed from", d)
		}
	}
	fmt.Println("services removed. Data kept in /var/lib/openv6* (delete manually if wanted).")
	return nil
}

func runResetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	data := fs.String("data", "/var/lib/openv6", "data directory")
	user := fs.String("user", "", "username (default: first admin)")
	_ = fs.Parse(args)
	st, err := store.Open(filepath.Join(*data, "openv6.db"))
	if err != nil {
		return err
	}
	if *user == "" {
		_ = st.DB.QueryRow(`SELECT username FROM users ORDER BY id LIMIT 1`).Scan(user)
	}
	pw := store.RandomToken(8)
	if err := st.SetPassword(*user, pw); err != nil {
		return fmt.Errorf("user %q: %w", *user, err)
	}
	fmt.Printf("user: %s\nnew password: %s\n", *user, pw)
	return nil
}

func runSelfTest() error {
	if os.Getenv("OPENV6_SELFTEST_INNER") == "1" {
		dir, _ := os.MkdirTemp("", "openv6-selftest")
		defer os.RemoveAll(dir)
		steps, err := netcfg.SelfTestBypass(dir)
		for _, s := range steps {
			fmt.Println("   ✓", s)
		}
		return err
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("run with sudo: sudo openv6 selftest")
	}
	fmt.Println("== 1) Mạng hiện tại / current network ==")
	m := netcfg.NewManager(filepath.Join(os.TempDir(), "openv6-selftest-ro"))
	st := m.Detect("")
	show := func(k, v string) {
		if v == "" {
			v = "—"
		}
		fmt.Printf("   %-26s %s\n", k, v)
	}
	show("Card mạng chính / primary:", st.PrimaryIface)
	for _, it := range st.Interfaces {
		if it.Name == st.PrimaryIface {
			show("Prefix IPv6:", strings.Join(it.Prefixes, ", "))
		}
	}
	show("Router IPv6:", st.Gateway)
	show("Đường ra hiện tại / egress:", st.EgressIface)
	show("IPv6 bị tắt / disabled:", fmt.Sprint(st.IPv6Disabled))
	var vpns []string
	for _, v := range st.VPNs {
		state := "down"
		if v.Up {
			state = "up"
		}
		vpns = append(vpns, fmt.Sprintf("%s (%s, %s)", v.Kind, v.Iface, state))
	}
	show("VPN:", strings.Join(vpns, "; "))
	if out, err := exec.Command("ip", "-6", "rule", "show", "table", netcfg.BypassTable).Output(); err == nil {
		show("Luật vượt VPN / bypass rules:", fmt.Sprint(strings.Count(string(out), "lookup")))
	}
	fmt.Println()
	fmt.Println("== 2) Thử định tuyến vượt VPN (mạng giả lập) / bypass routing test (isolated) ==")
	if _, err := exec.LookPath("unshare"); err != nil {
		return fmt.Errorf("unshare not found (package util-linux)")
	}
	self, _ := os.Executable()
	cmd := exec.Command("unshare", "-n", self, "selftest")
	cmd.Env = append(os.Environ(), "OPENV6_SELFTEST_INNER=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println("   ✗ KHÔNG ĐẠT / FAILED")
		return fmt.Errorf("bypass self-test failed: %v", err)
	}
	fmt.Println("   => ĐẠT / PASSED: proxy vẫn đi đúng card mạng khi bật VPN (WARP, Tailscale exit node, WireGuard…)")
	return nil
}
