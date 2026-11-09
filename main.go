// openv6: create and manage static IPv6 proxies (HTTP + SOCKS5) from the
// IPv6 prefix of a home network, VPS or homelab, with a central web panel.
// Runs on Linux, Windows and macOS: as a background service plus a tray icon.
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
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"openv6/internal/agent"
	"openv6/internal/logbuf"
	"openv6/internal/netcfg"
	"openv6/internal/panel"
	"openv6/internal/proto"
	"openv6/internal/service"
	"openv6/internal/store"
	"openv6/internal/tray"
)

//go:embed web
var webFS embed.FS

const usage = `openv6 %s - static IPv6 proxies with a web dashboard

Usage:
  openv6 server  [-listen :8080] [-data DIR] [-no-local-agent] [-tls-cert FILE -tls-key FILE]
      Run the web dashboard and the proxies of this machine.

  openv6 agent   -panel URL -token TOKEN [-data DIR]
      Run a node managed by a remote dashboard.

  openv6 install server|agent [same flags as above]
      Install as a service that starts at boot (run as root / Administrator),
      and the tray icon at login.

  openv6 uninstall
      Remove the service and every IPv6 address/route openv6 added.

  openv6 tray
      Tray icon: status, open dashboard, pause/resume, start at login.

  openv6 reset-password [-data DIR] [-user NAME]
  openv6 selftest        (Linux) check VPN-bypass routing in an isolated namespace
  openv6 version

Data directory: %s
`

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) < 2 {
		// double-clicking openv6.exe on Windows: install if needed, then tray
		if runtime.GOOS == "windows" {
			if err := guiStart(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		}
		fmt.Printf(usage, proto.Version, service.DataDir())
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(signalContext(), os.Args[2:])
	case "agent":
		err = runAgent(signalContext(), os.Args[2:])
	case "service-run": // started by the Windows service manager
		if len(os.Args) < 3 {
			err = fmt.Errorf("service-run needs a mode")
			break
		}
		mode, rest := os.Args[2], os.Args[3:]
		// a crash in the background service would otherwise leave no trace
		_ = os.MkdirAll(service.DataDir(), 0o700)
		if f, ferr := os.OpenFile(filepath.Join(service.DataDir(), "openv6-crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); ferr == nil {
			_ = debug.SetCrashOutput(f, debug.CrashOptions{})
		}
		err = service.RunService(func(ctx context.Context) error {
			if mode == "agent" {
				return runAgent(ctx, rest)
			}
			return runServer(ctx, rest)
		})
	case "install":
		err = runInstall(os.Args[2:])
	case "uninstall":
		err = runUninstall()
	case "tray":
		err = tray.Run()
	case "open": // open the dashboard, signed in (used by shortcuts)
		err = openApp()
	case "reset-password":
		err = runResetPassword(os.Args[2:])
	case "selftest":
		err = runSelfTest()
	case "version", "-v", "--version":
		fmt.Println(proto.Version)
	default:
		fmt.Printf(usage, proto.Version, service.DataDir())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if os.Args[1] == "service-run" { // no console: keep the reason in the log file
			if f, ferr := os.OpenFile(filepath.Join(service.DataDir(), "openv6.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); ferr == nil {
				fmt.Fprintf(f, "%s openv6 %s could not start: %v\n", time.Now().Format("2006/01/02 15:04:05"), proto.Version, err)
				f.Close()
			}
		}
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

// logToFile adds a log file in the data dir (Windows services have no console).
func logToFile(dir string, extra io.Writer) {
	w := []io.Writer{os.Stderr, extra}
	if runtime.GOOS == "windows" {
		if f, err := os.OpenFile(filepath.Join(dir, "openv6.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			if st, _ := f.Stat(); st != nil && st.Size() > 20<<20 {
				_ = f.Truncate(0)
			}
			w = append(w, f)
		}
	}
	log.SetOutput(io.MultiWriter(w...))
}

func runServer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", ":8080", "dashboard listen address")
	data := fs.String("data", service.DataDir(), "data directory")
	noLocal := fs.Bool("no-local-agent", false, "do not run proxies on this machine (dashboard only)")
	dry := fs.Bool("dry-run", false, "development: do not change networking")
	cert := fs.String("tls-cert", "", "TLS certificate file (enables HTTPS)")
	key := fs.String("tls-key", "", "TLS key file")
	_ = fs.Parse(args)

	if err := os.MkdirAll(*data, 0o700); err != nil {
		return err
	}
	logs := logbuf.New(5000)
	logToFile(*data, logs)
	st, err := store.Open(filepath.Join(*data, "openv6.db"))
	if err != nil {
		return err
	}
	ln, port, err := listenWithFallback(*listen, explicitFlag(fs, "listen"))
	if err != nil {
		return err
	}

	web, _ := fsSub(webFS, "web")
	exe, _ := os.Executable()
	srv := panel.New(st, web, *data, port, exe)
	srv.PanelLogs = logs
	srv.TrayToken = store.RandomToken(24)
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	srv.OnShutdown = func() {
		log.Printf("stopping: requested from the tray icon")
		service.StopSelf()
		stop()
	}
	go srv.RunBackground(ctx)

	scheme := "http"
	if *cert != "" {
		scheme = "https"
	}
	if err := service.WriteTrayInfo(service.TrayInfo{URL: fmt.Sprintf("%s://127.0.0.1:%d", scheme, port),
		Token: srv.TrayToken, Version: proto.Version, PID: os.Getpid()}); err != nil {
		log.Printf("tray info: %v", err)
	}

	if !*noLocal {
		local, err := st.LocalNode()
		if err == store.ErrNotFound {
			host, _ := os.Hostname()
			local, err = st.CreateNode("local ("+host+")", "", true)
		}
		if err != nil {
			return err
		}
		tok := local.Token
		ag := agent.New(agent.Config{DataDir: filepath.Join(*data, "agent"), DryRun: *dry, Token: tok,
			PanelURL: "local",
			Direct: func(ctx context.Context, req *proto.SyncRequest) (*proto.SyncResponse, error) {
				return srv.LocalSync(ctx, tok, req)
			}})
		go ag.Run(ctx)
	}

	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = hs.Shutdown(sctx)
	}()
	log.Printf("openv6 %s dashboard on %s (data %s)", proto.Version, ln.Addr(), *data)
	if *cert != "" {
		err = hs.ServeTLS(ln, *cert, *key)
	} else {
		err = hs.Serve(ln)
	}
	if err == http.ErrServerClosed {
		time.Sleep(300 * time.Millisecond) // let the local agent close listeners
		return nil
	}
	return err
}

func fsSub(f embed.FS, dir string) (fs.FS, error) { return fs.Sub(f, dir) }

func runAgent(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	panelURL := fs.String("panel", "", "dashboard URL, e.g. https://panel.example.com:8080")
	token := fs.String("token", "", "node token (from the dashboard)")
	data := fs.String("data", service.AgentDataDir(), "data directory")
	dry := fs.Bool("dry-run", false, "development: do not change networking")
	insecure := fs.Bool("insecure-tls", false, "skip TLS certificate verification (self-signed dashboard)")
	_ = fs.Parse(args)
	if *panelURL == "" || *token == "" {
		return fmt.Errorf("-panel and -token are required")
	}
	_ = os.MkdirAll(*data, 0o700)
	logs := logbuf.New(2000)
	logToFile(*data, logs)
	ag := agent.New(agent.Config{PanelURL: *panelURL, Token: *token, DataDir: *data, DryRun: *dry, InsecureTLS: *insecure,
		Logs: logs, AllowUpdate: true})
	ag.Run(ctx)
	return nil
}

func runInstall(args []string) error {
	if len(args) < 1 || (args[0] != "server" && args[0] != "agent") {
		return fmt.Errorf("usage: openv6 install server|agent [flags]")
	}
	if err := service.Install(args); err != nil {
		return err
	}
	if args[0] == "server" {
		fmt.Println("open http://<this-machine-ip>:8080 (or http://127.0.0.1:8080 here) to create the admin account")
	}
	return nil
}

func runUninstall() error {
	if err := service.Uninstall(); err != nil {
		return err
	}
	for _, d := range []string{filepath.Join(service.DataDir(), "agent"), service.AgentDataDir()} {
		if _, err := os.Stat(filepath.Join(d, "netstate.json")); err == nil {
			netcfg.NewManager(d).Cleanup()
			fmt.Println("removed IPv6 addresses installed from", d)
		}
	}
	fmt.Println("service removed. Data kept in", service.DataDir(), "(delete it manually if wanted).")
	return nil
}

func runResetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	data := fs.String("data", service.DataDir(), "data directory")
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

func explicitFlag(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// listenWithFallback opens the dashboard port. When the default port is
// taken by another program (common on Windows), the next free port in
// 8081-8099 is used; the tray learns the real port from tray.json.
func listenWithFallback(addr string, explicit bool) (net.Listener, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("bad -listen: %w", err)
	}
	port, _ := strconv.Atoi(portStr)
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			if loopbackReachesUs(ln, port) {
				return ln, port, nil
			}
			ln.Close()
			err = fmt.Errorf("127.0.0.1:%d is answered by another program", port)
		}
		lastErr = err
		if explicit || attempt >= 19 {
			// explicit port: wait for it to free up (service restarts, upgrades)
			time.Sleep(time.Second)
			continue
		}
		log.Printf("port %d is in use (%v), trying %d", port, err, port+1)
		port++
	}
	return nil, 0, fmt.Errorf("cannot open the dashboard port: %w", lastErr)
}

// loopbackReachesUs checks that 127.0.0.1:port really arrives at our
// listener. On Windows another program bound to 127.0.0.1:port takes those
// connections even though our wildcard bind succeeded, and the tray (which
// uses 127.0.0.1) would talk to the wrong program.
func loopbackReachesUs(ln net.Listener, port int) bool {
	tl, ok := ln.(*net.TCPListener)
	if !ok {
		return true
	}
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	local := c.LocalAddr().String()
	_ = tl.SetDeadline(time.Now().Add(time.Second))
	defer tl.SetDeadline(time.Time{})
	for {
		in, err := tl.Accept()
		if err != nil {
			return false
		}
		from := in.RemoteAddr().String()
		in.Close()
		if from == local {
			return true
		}
	}
}

// openApp is what the desktop/start-menu shortcut does: start openv6 again if
// it was quit from the tray, make sure the tray icon runs, open the dashboard.
func openApp() error {
	if service.Status() == "stopped" {
		if err := service.StartService(); err == nil {
			for i := 0; i < 40 && service.Status() != "running"; i++ {
				time.Sleep(500 * time.Millisecond)
			}
			time.Sleep(1500 * time.Millisecond) // dashboard listening, tray info written
		}
		if !tray.Running() {
			_ = exec.Command(service.BinPath(), "tray").Start()
		}
	}
	return tray.OpenDashboard("")
}
