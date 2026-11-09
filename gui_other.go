//go:build !windows

package main

import "fmt"

func guiStart() error { return fmt.Errorf("no command given") }
