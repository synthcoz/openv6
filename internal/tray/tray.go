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
	labels := func() {
		mOpen.SetTitle(tr("open"))
		mCheck.SetTitle(tr("check"))
		mReapply.SetTitle(tr("reapply"))
		mStart.SetTitle(tr("start_service"))
		mInstall.SetTitle(tr("install"))
		mLog.SetTitle(tr("view_log"))
		mReport.SetTitle(tr("report"))
		mAuto.SetTitle(tr("autostart"))
		mLang.SetTitle(tr("language"))
		mQuit.SetTitle(tr("quit"))
		if cur.Paused {
			mPause.SetTitle(tr("resume"))
		} else {
			mPause.SetTitle(tr("pause"))
		}
	}
	refresh := func() {
		var s status
		err := c.call("GET", "/api/tray/status", &s)
		var diagKey, diagDetail string
		if err != nil {
			diagKey, diagDetail = diagnose(err)
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
			mStart.Hide()
			mInstall.Hide()
			switch diagKey {
			case "diag_not_installed":
				mInstall.Show()
			case "diag_stopped", "diag_no_answer", "diag_no_info", "diag_unknown":
				mStart.Show()
			}
		case cur.Paused:
			st, line = icon.StatePaused, tr("paused", cur.Total)
			mStart.Hide()
		case cur.Problem != "" || !cur.Online:
			st, line = icon.StateError, tr("problem", cur.OK, cur.Total)
			mStart.Hide()
		case cur.Failing > 0 || cur.Errors > 0:
			st, line = icon.StateWarn, tr("some_failing", cur.OK, cur.Total)
			mStart.Hide()
		default:
			st, line = icon.StateOK, tr("all_ok", cur.OK, cur.Total)
			mStart.Hide()
		}
		systray.SetIcon(iconFor(st))
		systray.SetTooltip("openv6 — " + line)
		mStatus.SetTitle(line)
		if curOK && cur.Prefix != "" {
			mPrefix.SetTitle("IPv6: " + cur.Prefix)
			mPrefix.Show()
		} else {
			mPrefix.Hide()
		}
		switch {
		case curOK && cur.Problem != "":
			mProblem.SetTitle("⚠ " + tr("see_dashboard"))
			mProblem.Show()
		case !curOK && diagDetail != "":
			d := diagDetail
			if len(d) > 70 {
				d = d[:70] + "…"
			}
			mProblem.SetTitle(d)
			mProblem.Show()
		default:
			mProblem.Hide()
		}
		for _, m := range []*systray.MenuItem{mPause, mCheck, mReapply} {
			if curOK {
				m.Enable()
			} else {
				m.Disable()
			}
		}
		if curOK && cur.NewVersion != "" {
			mUpdate.SetTitle(tr("update", cur.NewVersion))
			mUpdate.Show()
		} else {
			mUpdate.Hide()
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
		if err := c.call("POST", "/api/tray/"+name, nil); err != nil {
			systray.SetTooltip("openv6 — " + err.Error())
		}
		refresh()
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
				go func() { _ = service.StartServiceElevated(); time.Sleep(3 * time.Second); refresh() }()
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
				systray.Quit()
				return
			}
		}
	}()
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
		"autostart": "Tự chạy khi đăng nhập", "language": "Ngôn ngữ", "quit": "Thoát biểu tượng (proxy vẫn chạy)",
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
		"autostart": "Start at login", "language": "Language", "quit": "Quit icon (proxies keep running)",
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
