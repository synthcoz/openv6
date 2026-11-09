//go:build windows

package main

import (
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"openv6/internal/service"
	"openv6/internal/tray"
)

// openv6.exe is built as a GUI program (no console window ever pops up).
// When a command is typed in a terminal we attach to that terminal so the
// output still shows.
func init() {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case "tray", "service-run", "open":
		return
	}
	const attachParent = ^uint32(0) // ATTACH_PARENT_PROCESS
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := k32.NewProc("AttachConsole").Call(uintptr(attachParent)); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}

func msgBox(title, text string, flags uint32) int32 {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _ := windows.MessageBox(0, m, t, flags|windows.MB_SETFOREGROUND)
	return r
}

func viLang() bool {
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	r, _, _ := k32.NewProc("GetUserDefaultUILanguage").Call()
	return r&0x3ff == 0x2a // LANG_VIETNAMESE
}

// guiStart is what happens when the user double-clicks openv6.exe:
// install (after confirmation) if needed, then show the tray and dashboard.
func guiStart() error {
	vi := viLang()
	pick := func(a, b string) string {
		if vi {
			return a
		}
		return b
	}
	if service.Status() == "not_installed" {
		r := msgBox("openv6", pick(
			"openv6 chưa được cài trên máy này.\n\nCài đặt ngay? openv6 sẽ chạy nền, tự khởi động cùng Windows và có biểu tượng ở khay hệ thống.",
			"openv6 is not installed on this computer.\n\nInstall now? openv6 will run in the background, start with Windows and show an icon in the system tray."),
			windows.MB_YESNO|windows.MB_ICONQUESTION)
		if r != 6 { // IDYES
			return nil
		}
		exe, _ := os.Executable()
		verb, _ := windows.UTF16PtrFromString("runas")
		file, _ := windows.UTF16PtrFromString(exe)
		args, _ := windows.UTF16PtrFromString("install server")
		if err := windows.ShellExecute(0, verb, file, args, nil, windows.SW_HIDE); err != nil {
			msgBox("openv6", pick("Chưa cài được: cần quyền Administrator.\n\n", "Install cancelled: Administrator rights are needed.\n\n")+err.Error(), windows.MB_ICONERROR)
			return nil
		}
		for i := 0; i < 60 && service.Status() != "running"; i++ {
			time.Sleep(time.Second)
		}
		if service.Status() != "running" {
			msgBox("openv6", pick("Dịch vụ openv6 chưa chạy được. Mở biểu tượng openv6 ở khay hệ thống → “Tạo báo cáo lỗi” để xem nguyên nhân.",
				"The openv6 service did not start. Open the openv6 tray icon → “Create error report” to see why."), windows.MB_ICONWARNING)
		} else {
			go func() { time.Sleep(3 * time.Second); _ = tray.OpenDashboard("") }()
		}
		// the installed copy owns the tray from now on
		installed := service.BinPath()
		if !strings.EqualFold(installed, exe) {
			cmd, _ := windows.UTF16PtrFromString(installed)
			ta, _ := windows.UTF16PtrFromString("tray")
			_ = windows.ShellExecute(0, nil, cmd, ta, nil, windows.SW_HIDE)
			return nil
		}
		return tray.Run()
	}
	if tray.Running() {
		return tray.OpenDashboard("")
	}
	return tray.Run()
}
