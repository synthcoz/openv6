//go:build linux

package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"openv6/internal/icon"
	"openv6/internal/netcfg"
)

const binPath = "/usr/local/bin/openv6"

func DataDir() string      { return "/var/lib/openv6" }
func AgentDataDir() string { return "/var/lib/openv6-agent" }
func TrayInfoPath() string { return "/run/openv6/tray.json" }
func BinPath() string      { return binPath }

// Managed reports whether we run under the service manager (systemd).
func Managed() bool { return os.Getenv("INVOCATION_ID") != "" }

const unitTmpl = `[Unit]
Description=openv6 %s
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%s %s
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`

// Install registers and starts the service. args are the mode followed by
// its flags, e.g. ["server", "-listen", ":8080"].
func Install(args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root (sudo)")
	}
	mode := args[0]
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := copyFile(exe, binPath, 0o755); err != nil {
		return err
	}
	var quoted []string
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	name := "openv6-" + mode
	unit := fmt.Sprintf(unitTmpl, mode, binPath, strings.Join(quoted, " "))
	if err := os.WriteFile("/etc/systemd/system/"+name+".service", []byte(unit), 0o644); err != nil {
		return err
	}
	// kernel limits for many addresses / connections, persistent across reboots
	if err := os.WriteFile("/etc/sysctl.d/99-openv6.conf", []byte(netcfg.SysctlConf()), 0o644); err == nil {
		_ = exec.Command("sysctl", "-q", "-p", "/etc/sysctl.d/99-openv6.conf").Run()
	}
	if mode == "server" && netcfg.UFWActive() {
		port := listenPort(args)
		for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10"} {
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
	if mode == "server" && hasDesktop() {
		if err := installDesktopEntry(); err == nil {
			if u := desktopUser(); u != nil {
				if err := setAutostart(u, true); err == nil {
					fmt.Println("tray icon will start at login for user " + u.Username)
				}
			}
		}
	}
	return nil
}

func listenPort(args []string) string {
	for i, a := range args {
		if (a == "-listen" || a == "--listen") && i+1 < len(args) {
			if _, p, err := net.SplitHostPort(args[i+1]); err == nil {
				return p
			}
		}
	}
	return "8080"
}

func Uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root (sudo)")
	}
	for _, n := range []string{"openv6-server", "openv6-agent"} {
		_ = exec.Command("systemctl", "disable", "--now", n).Run()
		_ = os.Remove("/etc/systemd/system/" + n + ".service")
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = os.Remove("/usr/share/applications/openv6.desktop")
	_ = os.Remove("/usr/share/icons/hicolor/256x256/apps/openv6.png")
	if u := desktopUser(); u != nil {
		_ = os.Remove(autostartPath(u))
	}
	_ = exec.Command("pkill", "-f", binPath+" tray").Run()
	return nil
}

// ---- tray autostart (per user, XDG) ----

func hasDesktop() bool {
	for _, d := range []string{"/usr/share/xsessions", "/usr/share/wayland-sessions"} {
		if ents, err := os.ReadDir(d); err == nil && len(ents) > 0 {
			return true
		}
	}
	return false
}

// desktopUser is the user who ran sudo (installer) or the current user.
func desktopUser() *user.User {
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		if u, err := user.Current(); err == nil && u.Uid != "0" {
			return u
		}
		return nil
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil
	}
	return u
}

func installDesktopEntry() error {
	_ = os.MkdirAll("/usr/share/icons/hicolor/256x256/apps", 0o755)
	if err := os.WriteFile("/usr/share/icons/hicolor/256x256/apps/openv6.png", icon.PNG(icon.StateOK, 256), 0o644); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=openv6\nComment=IPv6 proxy manager\nExec=" + binPath + " tray\nIcon=openv6\nTerminal=false\nCategories=Network;\n"
	return os.WriteFile("/usr/share/applications/openv6.desktop", []byte(entry), 0o644)
}

func autostartPath(u *user.User) string {
	return filepath.Join(u.HomeDir, ".config", "autostart", "openv6-tray.desktop")
}

func setAutostart(u *user.User, on bool) error {
	p := autostartPath(u)
	if !on {
		return os.Remove(p)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=openv6\nExec=" + binPath + " tray\nIcon=openv6\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	if err := os.WriteFile(p, []byte(entry), 0o644); err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	_ = os.Chown(filepath.Dir(p), uid, gid)
	return os.Chown(p, uid, gid)
}

// TrayAutostart reports / changes autostart of the tray for the current user.
func TrayAutostart() bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	_, err = os.Stat(autostartPath(u))
	return err == nil
}

func SetTrayAutostart(on bool) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	if !on {
		err := setAutostart(u, false)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return setAutostart(u, true)
}

// StartServiceElevated asks for admin rights and starts the service.
func StartServiceElevated() error {
	return exec.Command("pkexec", "systemctl", "restart", "openv6-server").Run()
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$`;&|<>()") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s) + `"`
}

// RunService runs fn directly (systemd supervises the process).
func RunService(fn func(ctx context.Context) error) error { return fn(context.Background()) }

func Status() string {
	out, _ := exec.Command("systemctl", "show", "-p", "LoadState,ActiveState", "openv6-server").Output()
	s := string(out)
	switch {
	case strings.Contains(s, "LoadState=not-found"):
		return "not_installed"
	case strings.Contains(s, "ActiveState=active"):
		return "running"
	case strings.Contains(s, "ActiveState=activating"):
		return "starting"
	case strings.Contains(s, "ActiveState="):
		return "stopped"
	}
	return "unknown"
}

// LogPath is empty: logs go to the systemd journal.
func LogPath() string { return "" }

func shareTrayDir(dir string) {}

// StartService starts the service, asking for admin rights.
func StartService() error { return StartServiceElevated() }

// StopSelf tells systemd the stop is intended, so Restart=always does not
// bring the service back until the next boot or start.
func StopSelf() {
	if os.Getenv("INVOCATION_ID") == "" { // not started by systemd
		return
	}
	_ = exec.Command("systemctl", "stop", "--no-block", "openv6-server").Run()
}

// SecureDataDir keeps the data folder private to root.
func SecureDataDir(dir string) { _ = os.Chmod(dir, 0o700) }
