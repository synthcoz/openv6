package tray

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"openv6/internal/proto"
	"openv6/internal/service"
)

const lockAddr = "127.0.0.1:47836"

// Running reports whether a tray is already running in this session.
func Running() bool {
	c, err := net.DialTimeout("tcp", lockAddr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// OpenDashboard opens the dashboard in the browser, signed in via a
// one-time link from the service. page: "", "nodes", "settings"…
func OpenDashboard(page string) error {
	info, err := service.ReadTrayInfo()
	if err != nil {
		openURL("http://127.0.0.1:8080/")
		return nil
	}
	req, _ := http.NewRequest("POST", info.URL+"/api/tray/login", bytes.NewReader(nil))
	req.Header.Set("X-Tray-Token", info.Token)
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		openURL(info.URL + "/")
		return nil
	}
	defer res.Body.Close()
	var r struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(res.Body).Decode(&r)
	u := r.URL
	if u == "" {
		u = info.URL + "/"
	}
	if page != "" {
		if strings.Contains(u, "?") {
			u += "&next=" + page
		} else {
			u += "#" + page
		}
	}
	openURL(u)
	return nil
}

// diagnose explains why the service cannot be reached.
// Returns a text key and the underlying detail.
func diagnose(callErr error) (key, detail string) {
	switch service.Status() {
	case "not_installed":
		return "diag_not_installed", ""
	case "stopped", "stopping":
		return "diag_stopped", ""
	case "starting":
		return "diag_starting", ""
	}
	if _, err := service.ReadTrayInfo(); err != nil {
		return "diag_no_info", err.Error()
	}
	if callErr != nil {
		return "diag_no_answer", callErr.Error()
	}
	return "diag_unknown", ""
}

// openLog opens the service log with the default viewer.
func openLog() {
	p := service.LogPath()
	if p == "" { // Linux: journal
		writeAndOpen("openv6-log.txt", journalTail(400))
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("notepad.exe", p).Start()
		return
	}
	openURL(p)
}

func journalTail(n int) string {
	out, err := exec.Command("journalctl", "-u", "openv6-server", "-n", fmt.Sprint(n), "--no-pager").CombinedOutput()
	if err != nil {
		return "journalctl: " + err.Error() + "\n" + string(out) + "\n(run: sudo journalctl -u openv6-server -n 200)"
	}
	return string(out)
}

// report writes a plain-text diagnostic report to the desktop (or home)
// and opens it, so the user can send it for support.
func report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "openv6 error report — %s\n\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "tray version: %s\nOS: %s/%s\n", proto.Version, runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "service status: %s\nbinary: %s\ndata dir: %s\n", service.Status(), service.BinPath(), service.DataDir())
	info, err := service.ReadTrayInfo()
	if err != nil {
		fmt.Fprintf(&b, "tray info: ERROR %v (%s)\n", err, service.TrayInfoPath())
	} else {
		fmt.Fprintf(&b, "tray info: url=%s version=%s pid=%d\n", info.URL, info.Version, info.PID)
		req, _ := http.NewRequest("GET", info.URL+"/api/tray/status", nil)
		req.Header.Set("X-Tray-Token", info.Token)
		if res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req); err != nil {
			fmt.Fprintf(&b, "status call: ERROR %v\n", err)
		} else {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 4000))
			res.Body.Close()
			fmt.Fprintf(&b, "status call: %s %s\n", res.Status, body)
		}
	}
	for _, port := range []int{8080, 8081, 8082} {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			c.Close()
			fmt.Fprintf(&b, "port %d: something is listening\n", port)
		} else {
			fmt.Fprintf(&b, "port %d: free/closed\n", port)
		}
	}
	if runtime.GOOS == "windows" {
		out, _ := exec.Command("sc.exe", "query", "openv6").CombinedOutput()
		fmt.Fprintf(&b, "\n--- sc query openv6\n%s\n", out)
		out, _ = exec.Command("sc.exe", "qc", "openv6").CombinedOutput()
		fmt.Fprintf(&b, "--- sc qc openv6\n%s\n", out)
	}
	b.WriteString("\n--- last log lines\n")
	if p := service.LogPath(); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			lines := strings.Split(string(data), "\n")
			if len(lines) > 300 {
				lines = lines[len(lines)-300:]
			}
			b.WriteString(strings.Join(lines, "\n"))
		} else {
			fmt.Fprintf(&b, "cannot read %s: %v\n", p, err)
		}
	} else {
		b.WriteString(journalTail(300))
	}
	return writeAndOpen("openv6-report.txt", b.String())
}

func writeAndOpen(name, content string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Desktop")
	if _, err := os.Stat(dir); err != nil {
		dir = home
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		p = filepath.Join(os.TempDir(), name)
		_ = os.WriteFile(p, []byte(content), 0o644)
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("notepad.exe", p).Start()
	} else {
		openURL(p)
	}
	return p
}

func openURL(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
