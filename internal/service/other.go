//go:build !linux && !windows && !darwin

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

var errUnsupported = errors.New("service install is not supported on this OS")

func DataDir() string                                     { return filepath.Join(os.TempDir(), "openv6") }
func AgentDataDir() string                                { return filepath.Join(os.TempDir(), "openv6-agent") }
func TrayInfoPath() string                                { return filepath.Join(DataDir(), "tray.json") }
func BinPath() string                                     { p, _ := os.Executable(); return p }
func Managed() bool                                       { return false }
func Install(args []string) error                         { return errUnsupported }
func Uninstall() error                                    { return errUnsupported }
func RunService(fn func(ctx context.Context) error) error { return fn(context.Background()) }
func TrayAutostart() bool                                 { return false }
func SetTrayAutostart(on bool) error                      { return errUnsupported }
func StartServiceElevated() error                         { return errUnsupported }
func StartService() error                                 { return errUnsupported }
func StopSelf()                                           {}

func Status() string      { return "unknown" }
func LogPath() string     { return "" }
func shareTrayDir(string) {}
