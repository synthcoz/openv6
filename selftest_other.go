//go:build !linux

package main

import "fmt"

func runSelfTest() error {
	return fmt.Errorf("selftest is only available on Linux; on this OS check the node self-diagnosis in the dashboard")
}
