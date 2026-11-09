//go:build darwin && !cgo

package tray

import "errors"

// Run needs a cgo build on macOS (Cocoa). Release binaries are built with cgo.
func Run() error {
	return errors.New("this macOS build has no tray support (build with CGO_ENABLED=1)")
}
