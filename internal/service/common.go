// Package service installs openv6 as an OS service that starts at boot
// (systemd, Windows Service, launchd) and the tray icon that starts at login.
package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// TrayInfo is written by the running service so the tray (running as the
// logged-in user) can find and talk to it. The token only grants the tray
// endpoints, which also require a loopback connection.
type TrayInfo struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

func WriteTrayInfo(ti TrayInfo) error {
	p := TrayInfoPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	_ = os.Chmod(filepath.Dir(p), 0o755)
	shareTrayDir(filepath.Dir(p))
	b, _ := json.MarshalIndent(ti, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func ReadTrayInfo() (TrayInfo, error) {
	var ti TrayInfo
	b, err := os.ReadFile(TrayInfoPath())
	if err != nil {
		return ti, err
	}
	err = json.Unmarshal(b, &ti)
	return ti, err
}

// Restart exits so the service manager starts the process again (systemd
// Restart=always, launchd KeepAlive, Windows recovery actions).
func Restart() {
	go func() {
		time.Sleep(time.Second)
		os.Exit(3)
	}()
}

// copyFile copies src to dst atomically with the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	if same(src, dst) {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		// Windows cannot replace a running executable: move it aside first
		_ = os.Remove(dst + ".old")
		if err2 := os.Rename(dst, dst+".old"); err2 != nil {
			return err
		}
		return os.Rename(tmp, dst)
	}
	return nil
}

func same(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}
