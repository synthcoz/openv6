package license

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OSMachineID returns the operating system's id of this computer:
// /etc/machine-id (Linux), MachineGuid (Windows), IOPlatformUUID (macOS).
func OSMachineID() string {
	switch runtime.GOOS {
	case "linux":
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
				return strings.TrimSpace(string(b))
			}
		}
	case "windows":
		return windowsMachineGUID()
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err == nil {
			for _, l := range strings.Split(string(out), "\n") {
				if strings.Contains(l, "IOPlatformUUID") {
					if f := strings.Split(l, "\""); len(f) >= 4 {
						return f[3]
					}
				}
			}
		}
	}
	return ""
}
