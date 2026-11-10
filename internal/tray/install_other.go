//go:build !windows

package tray

// installElevated opens the install guide (packages install the service).
func installElevated() error {
	openURL("https://thephuc.xyz/openv6/")
	return nil
}
