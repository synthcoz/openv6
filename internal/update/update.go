// Package update checks GitHub releases and replaces the running binary.
package update

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Release struct {
	Tag    string            `json:"tag"`
	Notes  string            `json:"notes"`
	URL    string            `json:"url"`
	Assets map[string]string `json:"-"` // asset name -> API download URL
}

// Latest returns the newest release of repo ("owner/name").
func Latest(ctx context.Context, repo string) (*Release, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("github: %s", res.Status)
	}
	var j struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&j); err != nil {
		return nil, err
	}
	r := &Release{Tag: j.TagName, Notes: j.Body, URL: j.HTMLURL, Assets: map[string]string{}}
	for _, a := range j.Assets {
		r.Assets[a.Name] = a.URL
	}
	return r, nil
}

// AssetName is the release file name of the binary for a platform.
func AssetName(goos, arch string) string {
	if goos == "windows" {
		return "openv6-windows-" + arch + ".exe"
	}
	return "openv6-" + goos + "-" + arch
}

// TrayAssetName is the Windows GUI build used for the tray icon.
func TrayAssetName(arch string) string { return "openv6-tray-windows-" + arch + ".exe" }

// Platforms published in releases.
var Platforms = [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"linux", "arm"},
	{"windows", "amd64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

// Newer reports whether version a (e.g. "v0.2.0" or "0.2.0") is newer than b.
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	for i, p := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		out[i], _ = strconv.Atoi(strings.TrimFunc(p, func(r rune) bool { return r < '0' || r > '9' }))
	}
	return out
}

// Download fetches url into a temp file next to dst. GitHub API asset URLs
// need the octet-stream Accept header.
func Download(ctx context.Context, url, dst string, insecure bool) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	if strings.Contains(url, "api.github.com") {
		req.Header.Set("Accept", "application/octet-stream")
	}
	cl := &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}}}
	var res *http.Response
	for i := 0; i < 3; i++ {
		res, err = cl.Do(req)
		if err == nil && res.StatusCode == 200 {
			break
		}
		if err == nil {
			res.Body.Close()
			err = fmt.Errorf("download: %s", res.Status)
		}
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	pattern := ".openv6-update-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe" // Windows only runs files with an executable extension
	}
	f, err := os.CreateTemp(filepath.Dir(dst), pattern)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, io.LimitReader(res.Body, 200<<20)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// Install verifies that tmp is a working openv6 binary (optionally of the
// expected version) and atomically replaces dst with it.
func Install(tmp, dst, expect string) error {
	defer os.Remove(tmp)
	b := make([]byte, 4)
	f, err := os.Open(tmp)
	if err != nil {
		return err
	}
	_, _ = io.ReadFull(f, b)
	f.Close()
	if runtimeOSOf(b) == "" {
		return errors.New("downloaded file is not a program for this computer")
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == runtimeOSOf(b) {
		out, err := exec.Command(tmp, "version").Output()
		if err != nil {
			return fmt.Errorf("new binary does not run: %v", err)
		}
		got := strings.TrimSpace(string(out))
		if expect != "" && strings.TrimPrefix(got, "v") != strings.TrimPrefix(expect, "v") {
			return fmt.Errorf("new binary reports version %s, expected %s", got, expect)
		}
	}
	keep := tmp + ".keep"
	if err := os.Link(tmp, keep); err != nil {
		keep = tmp
	}
	defer os.Remove(keep)
	if err := os.Rename(keep, dst); err != nil {
		// Windows: a running .exe cannot be replaced but can be renamed
		_ = os.Remove(dst + ".old")
		if err2 := os.Rename(dst, dst+".old"); err2 != nil {
			return err
		}
		return os.Rename(keep, dst)
	}
	return nil
}

// runtimeOSOf recognises the executable format from its first bytes.
func runtimeOSOf(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x7fELF")):
		return "linux"
	case bytes.HasPrefix(b, []byte("MZ")):
		return "windows"
	case bytes.HasPrefix(b, []byte{0xcf, 0xfa, 0xed, 0xfe}), bytes.HasPrefix(b, []byte{0xca, 0xfe, 0xba, 0xbe}):
		return "darwin"
	}
	return ""
}

// SelfPath is the path of the running executable.
func SelfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}
