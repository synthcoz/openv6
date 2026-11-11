//go:build !darwin || cgo

// Package tray is the system tray icon: it shows the service status with a
// coloured icon and lets the user open the dashboard, pause/resume proxies,
// run checks, re-apply the network and toggle start at login.
package tray

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"openv6/internal/icon"
	"openv6/internal/service"
)

// Run shows the tray icon until the user quits it. Only one tray runs per
// user session (a loopback port acts as the lock).
func Run() error {
	lock, err := net.Listen("tcp", lockAddr)
	if err != nil {
		return nil // already running
	}
	defer lock.Close()
	systray.Run(onReady, func() {})
	return nil
}

type status struct {
	Version    string `json:"version"`
	Lang       string `json:"lang"`
	Online     bool   `json:"online"`
	Paused     bool   `json:"paused"`
	Total      int    `json:"total"`
	OK         int    `json:"ok"`
	Failing    int    `json:"failing"`
	Errors     int    `json:"errors"`
	Prefix     string `json:"prefix"`
	Problem    string `json:"problem"`
	NewVersion string `json:"new_version"`
	Setup      bool   `json:"setup_required"`
}

type client struct {
	hc   *http.Client
	mu   sync.Mutex
	info service.TrayInfo
}

func (c *client) call(method, path string, out any) error {
	info, err := service.ReadTrayInfo()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.info = info
	c.mu.Unlock()
	req, _ := http.NewRequest(method, info.URL+path, bytes.NewReader(nil))
	req.Header.Set("X-Tray-Token", info.Token)
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func iconFor(st icon.State) []byte {
	if runtime.GOOS == "windows" {
		return icon.ICO(st, 16, 32, 48)
	}
	return icon.PNG(st, 64)
}

func onReady() {
	c := &client{hc: &http.Client{Timeout: 5 * time.Second}}
	lang := detectLang()
	tr := func(k string, a ...any) string { return t(lang, k, a...) }

	systray.SetIcon(iconFor(icon.StateBrand))
	systray.SetTitle("")
	systray.SetTooltip("openv6")

	mStatus := systray.AddMenuItem("openv6", "")
	mStatus.Disable()
	mPrefix := systray.AddMenuItem("", "")
	mPrefix.Disable()
	mProblem := systray.AddMenuItem("", "")
	mProblem.Disable()
	mProblem.Hide()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("", "")
	mPause := systray.AddMenuItem("", "")
	mCheck := systray.AddMenuItem("", "")
	mReapply := systray.AddMenuItem("", "")
	mStart := systray.AddMenuItem("", "")
	mStart.Hide()
	mInstall := systray.AddMenuItem("", "")
	mInstall.Hide()
	mLog := systray.AddMenuItem("", "")
	mReport := systray.AddMenuItem("", "")
	systray.AddSeparator()
	mAuto := systray.AddMenuItemCheckbox("", "", service.TrayAutostart())
	mLang := systray.AddMenuItem("", "")
	mLangVI := mLang.AddSubMenuItemCheckbox("Tiếng Việt", "", lang == "vi")
	mLangEN := mLang.AddSubMenuItemCheckbox("English", "", lang == "en")
	mUpdate := systray.AddMenuItem("", "")
	mUpdate.Hide()
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("", "")

	var cur status
	var curOK bool
	// Every Set* call redraws the icon or menu in the taskbar, and doing that
	// every few seconds makes the Windows tray icon flicker. The ui helpers
	// only touch what actually changed.
	var mu sync.Mutex
	lastIcon, lastTip := icon.State(-1), ""
	titles := map[*systray.MenuItem]string{}
	shown := map[*systray.MenuItem]bool{}
	enabled := map[*systray.MenuItem]bool{}
	setIcon := func(st icon.State) {
		if st != lastIcon {
			lastIcon = st
			systray.SetIcon(iconFor(st))
		}
	}
	setTip := func(s string) {
		if s != lastTip {
			lastTip = s
			systray.SetTooltip(s)
		}
	}
	setTitle := func(m *systray.MenuItem, s string) {
		if titles[m] != s {
			titles[m] = s
			m.SetTitle(s)
		}
	}
	setShown := func(m *systray.MenuItem, on bool) {
		if v, ok := shown[m]; !ok || v != on {
			shown[m] = on
			if on {
				m.Show()
			} else {
				m.Hide()
			}
		}
	}
	setEnabled := func(m *systray.MenuItem, on bool) {
		if v, ok := enabled[m]; !ok || v != on {
			enabled[m] = on
			if on {
				m.Enable()
			} else {
				m.Disable()
			}
		}
	}
	lastIcon = icon.StateBrand
	for _, m := range []*systray.MenuItem{mProblem, mStart, mInstall, mUpdate} {
		shown[m] = false
	}
	failures := 0 // consecutive failed status calls
	labels := func() {
		setTitle(mOpen, tr("open"))
		setTitle(mCheck, tr("check"))
		setTitle(mReapply, tr("reapply"))
		setTitle(mStart, tr("start_service"))
		setTitle(mInstall, tr("install"))
		setTitle(mLog, tr("view_log"))
		setTitle(mReport, tr("report"))
		setTitle(mAuto, tr("autostart"))
		setTitle(mLang, tr("language"))
		setTitle(mQuit, tr("quit"))
		if cur.Paused {
			setTitle(mPause, tr("resume"))
		} else {
			setTitle(mPause, tr("pause"))
		}
	}
	refresh := func() {
		mu.Lock()
		defer mu.Unlock()
		var s status
		err := c.call("GET", "/api/tray/status", &s)
		var diagKey, diagDetail string
		if err != nil {
			failures++
			if curOK && failures < 3 {
				return // one slow answer (busy service, sleep/resume) is not an outage: keep the icon
			}
			diagKey, diagDetail = diagnose(err)
		} else {
			failures = 0
		}
		curOK = err == nil
		if err == nil {
			cur = s
			if s.Lang != "" && s.Lang != lang {
				lang = s.Lang
				mLangVI.Uncheck()
				mLangEN.Uncheck()
				if lang == "vi" {
					mLangVI.Check()
				} else {
					mLangEN.Check()
				}
			}
		}
		labels()
		var st icon.State
		var line string
		switch {
		case !curOK:
			st, line = icon.StateError, tr(diagKey)
			setShown(mStart, false)
			setShown(mInstall, false)
			switch diagKey {
			case "diag_not_installed":
				setShown(mInstall, true)
			case "diag_stopped", "diag_no_answer", "diag_no_info", "diag_unknown":
				setShown(mStart, true)
			}
		case cur.Paused:
			st, line = icon.StatePaused, tr("paused", cur.Total)
			setShown(mStart, false)
		case cur.Problem != "" || !cur.Online:
			st, line = icon.StateError, tr("problem", cur.OK, cur.Total)
			setShown(mStart, false)
		case cur.Failing > 0 || cur.Errors > 0:
			st, line = icon.StateWarn, tr("some_failing", cur.OK, cur.Total)
			setShown(mStart, false)
		default:
			st, line = icon.StateOK, tr("all_ok", cur.OK, cur.Total)
			setShown(mStart, false)
		}
		setIcon(st)
		setTip("openv6 — " + line)
		setTitle(mStatus, line)
		if curOK && cur.Prefix != "" {
			setTitle(mPrefix, "IPv6: "+cur.Prefix)
			setShown(mPrefix, true)
		} else {
			setShown(mPrefix, false)
		}
		switch {
		case curOK && cur.Problem != "":
			setTitle(mProblem, "⚠ "+tr("see_dashboard"))
			setShown(mProblem, true)
		case !curOK && diagDetail != "":
			d := diagDetail
			if len(d) > 70 {
				d = d[:70] + "…"
			}
			setTitle(mProblem, d)
			setShown(mProblem, true)
		default:
			setShown(mProblem, false)
		}
		for _, m := range []*systray.MenuItem{mPause, mCheck, mReapply} {
			setEnabled(m, curOK)
		}
		if curOK && cur.NewVersion != "" {
			setTitle(mUpdate, tr("update", cur.NewVersion))
			setShown(mUpdate, true)
		} else {
			setShown(mUpdate, false)
		}
	}
	refresh()
	go func() {
		for range time.Tick(4 * time.Second) {
			refresh()
		}
	}()

	openDashboard := func(page string) { _ = OpenDashboard(page) }
	action := func(name string) {
		err := c.call("POST", "/api/tray/"+name, nil)
		refresh()
		if err != nil {
			mu.Lock()
			setTip("openv6 — " + err.Error())
			mu.Unlock()
		}
	}

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openDashboard("")
			case <-mStatus.ClickedCh:
				openDashboard("")
			case <-mProblem.ClickedCh:
				if curOK {
					openDashboard("nodes")
				} else {
					go report()
				}
			case <-mLog.ClickedCh:
				go openLog()
			case <-mReport.ClickedCh:
				go report()
			case <-mInstall.ClickedCh:
				go func() { _ = installElevated(); time.Sleep(5 * time.Second); refresh() }()
			case <-mPause.ClickedCh:
				if cur.Paused {
					action("resume")
				} else {
					action("pause")
				}
			case <-mCheck.ClickedCh:
				action("check")
			case <-mReapply.ClickedCh:
				action("reapply")
			case <-mStart.ClickedCh:
				go func() { _ = service.StartService(); time.Sleep(3 * time.Second); refresh() }()
			case <-mAuto.ClickedCh:
				on := !mAuto.Checked()
				if err := service.SetTrayAutostart(on); err == nil {
					if on {
						mAuto.Check()
					} else {
						mAuto.Uncheck()
					}
				}
			case <-mLangVI.ClickedCh:
				lang = "vi"
				mLangVI.Check()
				mLangEN.Uncheck()
				action("lang-vi")
			case <-mLangEN.ClickedCh:
				lang = "en"
				mLangEN.Check()
				mLangVI.Uncheck()
				action("lang-en")
			case <-mUpdate.ClickedCh:
				openDashboard("settings")
			case <-mQuit.ClickedCh:
				// Quit stops everything: proxies, the service and the icon.
				// Opening openv6 again (or the next boot) starts it again.
				mu.Lock()
				setTitle(mStatus, tr("stopping"))
				setTip("openv6 — " + tr("stopping"))
				mu.Unlock()
				quitAll(c)
				systray.Quit()
				return
			}
		}
	}()
}

// quitAll asks the service to stop (proxies included) and waits until it has,
// so "Quit" never leaves proxies running without an icon. It tries even when
// the last status call failed: a slow answer is not a stopped service.
func quitAll(c *client) {
	for i := 0; i < 3; i++ {
		if err := c.call("POST", "/api/tray/shutdown", nil); err == nil {
			break
		}
		if st := service.Status(); st == "stopped" || st == "not_installed" {
			return
		}
		time.Sleep(time.Second)
	}
	for i := 0; i < 20; i++ { // up to ~10 s for proxies to close
		if st := service.Status(); st != "running" && st != "starting" && st != "stopping" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func detectLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(v, "vi") {
				return "vi"
			}
			return "en"
		}
	}
	return "vi"
}

var texts = map[string]map[string]string{
	"vi": {
		"open": "Mở bảng điều khiển", "pause": "Tạm dừng tất cả proxy", "resume": "Tiếp tục chạy proxy",
		"check": "Kiểm tra tất cả proxy", "reapply": "Áp dụng lại mạng", "start_service": "Khởi động dịch vụ openv6",
		"autostart": "Tự chạy khi đăng nhập", "language": "Ngôn ngữ", "quit": "Thoát openv6 (tắt proxy)", "stopping": "Đang tắt openv6…",
		"service_down": "Dịch vụ openv6 chưa chạy", "paused": "Đã tạm dừng %d proxy",
		"diag_not_installed": "openv6 chưa được cài đặt", "diag_stopped": "Dịch vụ openv6 đang dừng",
		"diag_starting": "Dịch vụ openv6 đang khởi động…", "diag_no_info": "Không đọc được thông tin kết nối của dịch vụ",
		"diag_no_answer": "Dịch vụ chạy nhưng không phản hồi", "diag_unknown": "Không kết nối được dịch vụ openv6",
		"install": "Cài đặt openv6…", "view_log": "Xem nhật ký (log)", "report": "Tạo báo cáo lỗi",
		"problem": "Có sự cố — %d/%d proxy hoạt động", "some_failing": "%d/%d proxy hoạt động",
		"all_ok": "%d/%d proxy hoạt động", "see_dashboard": "Có sự cố, bấm để xem cách sửa", "update": "Có bản mới %s — cập nhật",
	},
	"en": {
		"open": "Open dashboard", "pause": "Pause all proxies", "resume": "Resume proxies",
		"check": "Check all proxies", "reapply": "Re-apply network", "start_service": "Start the openv6 service",
		"autostart": "Start at login", "language": "Language", "quit": "Quit openv6 (stops proxies)", "stopping": "Stopping openv6…",
		"service_down": "openv6 service is not running", "paused": "%d proxies paused",
		"diag_not_installed": "openv6 is not installed", "diag_stopped": "openv6 service is stopped",
		"diag_starting": "openv6 service is starting…", "diag_no_info": "Cannot read the service connection info",
		"diag_no_answer": "Service running but not answering", "diag_unknown": "Cannot reach the openv6 service",
		"install": "Install openv6…", "view_log": "View log", "report": "Create error report",
		"problem": "Problem — %d/%d proxies working", "some_failing": "%d/%d proxies working",
		"all_ok": "%d/%d proxies working", "see_dashboard": "Problem detected, click for the fix", "update": "New version %s — update",
	},
}

func t(lang, k string, a ...any) string {
	s := texts[lang][k]
	if s == "" {
		s = texts["en"][k]
	}
	if len(a) > 0 {
		return fmt.Sprintf(s, a...)
	}
	return s
}
