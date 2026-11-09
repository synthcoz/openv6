//go:build darwin

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const binPath = "/usr/local/bin/openv6"

func DataDir() string      { return "/Library/Application Support/openv6" }
func AgentDataDir() string { return "/Library/Application Support/openv6-agent" }
func TrayInfoPath() string { return "/Library/Application Support/openv6/tray.json" }
func BinPath() string      { return binPath }

// Managed reports whether launchd started us from our plist.
func Managed() bool { return strings.HasPrefix(os.Getenv("XPC_SERVICE_NAME"), "com.openv6") }

func RunService(fn func(ctx context.Context) error) error { return fn(context.Background()) }

const daemonPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array>%s</array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/Library/Logs/openv6.log</string>
  <key>StandardOutPath</key><string>/Library/Logs/openv6.log</string>
</dict></plist>
`

func plistArgs(args []string) string {
	var sb strings.Builder
	for _, a := range args {
		sb.WriteString("<string>" + strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(a) + "</string>")
	}
	return sb.String()
}

func Install(args []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root (sudo)")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := copyFile(exe, binPath, 0o755); err != nil {
		return err
	}
	label := "com.openv6." + args[0]
	p := "/Library/LaunchDaemons/" + label + ".plist"
	_ = exec.Command("launchctl", "bootout", "system/"+label).Run()
	if err := os.WriteFile(p, []byte(fmt.Sprintf(daemonPlist, label, plistArgs(append([]string{binPath}, args...)))), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("launchctl", "bootstrap", "system", p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v %s", err, out)
	}
	fmt.Println("installed and started " + label)
	if args[0] == "server" {
		if u := consoleUser(); u != nil {
			if err := setAutostartFor(u, true); err == nil {
				fmt.Println("tray icon will start at login for user " + u.Username)
			}
		}
	}
	return nil
}

func Uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root (sudo)")
	}
	for _, mode := range []string{"server", "agent"} {
		label := "com.openv6." + mode
		_ = exec.Command("launchctl", "bootout", "system/"+label).Run()
		_ = os.Remove("/Library/LaunchDaemons/" + label + ".plist")
	}
	if u := consoleUser(); u != nil {
		_ = setAutostartFor(u, false)
	}
	_ = exec.Command("pkill", "-f", binPath+" tray").Run()
	return nil
}

// consoleUser is the user logged in at the Mac (installer runs as root).
func consoleUser() *user.User {
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		if out, err := exec.Command("stat", "-f%Su", "/dev/console").Output(); err == nil {
			name = strings.TrimSpace(string(out))
		}
	}
	if name == "" || name == "root" {
		return nil
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil
	}
	return u
}

const agentPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.openv6.tray</string>
  <key>ProgramArguments</key><array><string>` + binPath + `</string><string>tray</string></array>
  <key>RunAtLoad</key><true/>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
</dict></plist>
`

func trayPlistPath(u *user.User) string {
	return filepath.Join(u.HomeDir, "Library", "LaunchAgents", "com.openv6.tray.plist")
}

func setAutostartFor(u *user.User, on bool) error {
	p := trayPlistPath(u)
	if !on {
		err := os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(agentPlist), 0o644); err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return os.Chown(p, uid, gid)
}

func TrayAutostart() bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	_, err = os.Stat(trayPlistPath(u))
	return err == nil
}

func SetTrayAutostart(on bool) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	return setAutostartFor(u, on)
}

func StartServiceElevated() error {
	return exec.Command("osascript", "-e",
		`do shell script "launchctl kickstart -k system/com.openv6.server" with administrator privileges`).Run()
}

func Status() string {
	if _, err := os.Stat("/Library/LaunchDaemons/com.openv6.server.plist"); err != nil {
		return "not_installed"
	}
	out, err := exec.Command("launchctl", "print", "system/com.openv6.server").Output()
	if err != nil {
		return "stopped"
	}
	if strings.Contains(string(out), "state = running") {
		return "running"
	}
	return "stopped"
}

func LogPath() string { return "/Library/Logs/openv6.log" }

func shareTrayDir(dir string) {}
