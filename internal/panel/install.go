package panel

import (
	"fmt"
	"net/http"
	"openv6/internal/update"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func normArch(a string) string {
	switch strings.ToLower(a) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l", "armv7", "arm":
		return "arm"
	}
	return ""
}

// handleDownload serves the agent binary for an OS/arch. The panel's own
// binary is always the freshest for its platform; others come from
// <data>/bin (filled by "Update" or copied there by hand).
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	goos := strings.ToLower(r.PathValue("os"))
	if goos == "" {
		goos = "linux"
	}
	arch := normArch(r.PathValue("arch"))
	if arch == "" || (goos != "linux" && goos != "windows" && goos != "darwin") {
		http.Error(w, "unsupported platform", 404)
		return
	}
	var p string
	if runtime.GOOS == goos && runtime.GOARCH == arch && s.SelfExe != "" {
		p = s.SelfExe
	} else {
		p = filepath.Join(s.DataDir, "bin", update.AssetName(goos, arch))
		if _, err := os.Stat(p); err != nil {
			http.Error(w, fmt.Sprintf("no binary for %s/%s on this panel: copy it to %s", goos, arch, p), 404)
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, p)
}

// handleInstallPS1 installs a Windows node: run in an Administrator
// PowerShell:  & ([scriptblock]::Create((irm PANEL/install.ps1))) -Token TOKEN
func (s *Server) handleInstallPS1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, strings.ReplaceAll(installPS1, "__PANEL__", s.panelURL(r)))
}

const installPS1 = `param([Parameter(Mandatory=$true)][string]$Token, [string]$Panel = "__PANEL__")
$ErrorActionPreference = "Stop"
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { Write-Host "Hay mo PowerShell bang 'Run as Administrator' / Run PowerShell as Administrator" -ForegroundColor Red; return }
$arch = if ([Environment]::Is64BitOperatingSystem) { if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" } } else { "386" }
$tmp = Join-Path $env:TEMP "openv6-setup.exe"
Write-Host ">> downloading openv6 for windows/$arch"
Invoke-WebRequest -UseBasicParsing -Uri "$Panel/download/windows/$arch" -OutFile $tmp
& $tmp install agent -panel $Panel -token $Token
`

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	fmt.Fprint(w, strings.ReplaceAll(installScript, "__PANEL__", s.panelURL(r)))
}

const installScript = `#!/usr/bin/env bash
# openv6 installer: downloads the binary from the panel and registers a systemd service.
# usage: curl -fsSL __PANEL__/install.sh | sudo bash -s -- agent --panel URL --token TOKEN
set -euo pipefail
PANEL="__PANEL__"
if [ "$(id -u)" != "0" ]; then echo "please run as root (sudo)"; exit 1; fi
if [ "$(uname -s)" != "Linux" ]; then echo "openv6 nodes require Linux"; exit 1; fi
ARCH="$(uname -m)"
if ! command -v ip >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then apt-get update -qq && apt-get install -y -qq iproute2;
  elif command -v dnf >/dev/null 2>&1; then dnf install -y iproute;
  elif command -v apk >/dev/null 2>&1; then apk add iproute2;
  else echo "please install iproute2 first"; exit 1; fi
fi
echo ">> downloading openv6 for $ARCH"
curl -fsSL "$PANEL/download/$ARCH" -o /usr/local/bin/openv6.new
chmod +x /usr/local/bin/openv6.new
mv -f /usr/local/bin/openv6.new /usr/local/bin/openv6
/usr/local/bin/openv6 install "$@"
`
