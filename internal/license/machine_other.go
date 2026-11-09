//go:build !windows

package license

func windowsMachineGUID() string { return "" }
