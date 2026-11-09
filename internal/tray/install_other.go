//go:build !windows

package tray

// installElevated opens the install guide (packages install the service).
func installElevated() error {
	openURL("https://github.com/synthcoz/openv6#cài-đặt")
	return nil
}
