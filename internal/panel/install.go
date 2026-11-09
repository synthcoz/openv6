package panel

import (
	"fmt"
	"net/http"
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

// handleDownload serves the agent binary. Binaries for other architectures can
// be dropped into <data>/bin/openv6-linux-<arch>.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	arch := normArch(r.PathValue("arch"))
	if arch == "" {
		http.Error(w, "unsupported architecture", 404)
		return
	}
	p := filepath.Join(s.DataDir, "bin", "openv6-linux-"+arch)
	if _, err := os.Stat(p); err != nil {
		if runtime.GOOS == "linux" && runtime.GOARCH == arch && s.SelfExe != "" {
			p = s.SelfExe
		} else {
			http.Error(w, fmt.Sprintf("no binary for linux/%s on this panel: copy it to %s", arch, p), 404)
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, p)
}

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
