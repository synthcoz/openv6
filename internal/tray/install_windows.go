//go:build windows

package tray

import (
	"os"

	"golang.org/x/sys/windows"
)

// installElevated re-runs this program as Administrator to install the service.
func installElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString("install server")
	return windows.ShellExecute(0, verb, file, args, nil, windows.SW_HIDE)
}
