//go:build windows

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const svcName = "openv6"

func programData() string {
	if p := os.Getenv("ProgramData"); p != "" {
		return p
	}
	return `C:\ProgramData`
}

func DataDir() string      { return filepath.Join(programData(), "openv6") }
func AgentDataDir() string { return filepath.Join(programData(), "openv6", "agent-only") }
func TrayInfoPath() string { return filepath.Join(programData(), "openv6", "tray.json") }

func BinPath() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "openv6", "openv6.exe")
}

// TrayExe is the GUI build (no console window) used for the tray icon;
// falls back to the main binary.
func TrayExe() string {
	p := filepath.Join(filepath.Dir(BinPath()), "openv6-tray.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return BinPath()
}

// Managed reports whether we run as a Windows service.
func Managed() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

// Install registers openv6 as an automatic Windows service with restart on
// failure, starts it and enables the tray at login for the current user.
func Install(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dst := BinPath()
	if err := copyFile(exe, dst, 0o755); err != nil {
		return fmt.Errorf("copy to %s: %w", dst, err)
	}
	if t := filepath.Join(filepath.Dir(exe), "openv6-tray.exe"); fileExists(t) {
		_ = copyFile(t, filepath.Join(filepath.Dir(dst), "openv6-tray.exe"), 0o755)
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	svcArgs := append([]string{"service-run"}, args...)
	if s, err := m.OpenService(svcName); err == nil {
		_, _ = s.Control(svc.Stop)
		waitStopped(s)
		_ = s.Delete()
		s.Close()
		time.Sleep(time.Second)
	}
	s, err := m.CreateService(svcName, dst, mgr.Config{
		DisplayName: "openv6 IPv6 proxy",
		Description: "Static IPv6 proxies (HTTP + SOCKS5) with a web dashboard.",
		StartType:   mgr.StartAutomatic,
	}, svcArgs...)
	if err != nil {
		return err
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 3 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
	}, 3600)
	if err := s.Start(); err != nil {
		return err
	}
	fmt.Println("installed and started the 'openv6' Windows service")
	if args[0] == "server" {
		if err := SetTrayAutostart(true); err == nil {
			fmt.Println("tray icon will start at login")
		}
	}
	return nil
}

func waitStopped(s *mgr.Service) {
	for i := 0; i < 20; i++ {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(svcName); err == nil {
		_, _ = s.Control(svc.Stop)
		waitStopped(s)
		_ = s.Delete()
		s.Close()
	}
	_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name=openv6").Run()
	_ = SetTrayAutostart(false)
	_ = exec.Command("taskkill", "/F", "/IM", "openv6-tray.exe").Run()
	return nil
}

// RunService runs fn under the Windows service control manager until it is
// asked to stop.
func RunService(fn func(ctx context.Context) error) error {
	return svc.Run(svcName, &handler{fn: fn})
}

type handler struct {
	fn func(ctx context.Context) error
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.fn(ctx) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// ---- tray autostart: HKCU\Software\Microsoft\Windows\CurrentVersion\Run ----

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func TrayAutostart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("openv6")
	return err == nil
}

func SetTrayAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		err := k.DeleteValue("openv6")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	return k.SetStringValue("openv6", `"`+TrayExe()+`" tray`)
}

// StartServiceElevated shows the UAC prompt and (re)starts the service.
func StartServiceElevated() error {
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command",
		"Start-Process -Verb RunAs -WindowStyle Hidden -FilePath sc.exe -ArgumentList 'start','openv6'")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
