package main

import (
	"syscall"

	"github.com/getlantern/systray"
	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/server"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	showWindowProc   = user32.NewProc("ShowWindow")
	allocConsole     = kernel32.NewProc("AllocConsole")
)

const (
	swHide = 0
	swShow = 5
)

func hideConsole() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		showWindowProc.Call(hwnd, swHide)
	}
}

func getTrayLabels() (openTitle, quitTitle string) {
	userDefaultUILang := kernel32.NewProc("GetUserDefaultUILanguage")
	ret, _, _ := userDefaultUILang.Call()
	primaryLangId := uint16(ret) & 0x3ff
	if primaryLangId == 0x04 { // LANG_CHINESE
		return "打开仪表盘", "退出"
	}
	return "Open Dashboard", "Exit"
}

func setTrayIcon() {
	systray.SetIcon(iconData)
}

func platformRun(srv *server.Server, cfg *config.Config, showConsole bool, shutdown func()) error {
	// Windows 控制台处理
	if showConsole {
		hwnd, _, _ := getConsoleWindow.Call()
		if hwnd == 0 {
			// 如果当前没有控制台（通常是 GUI 模式双击启动），尝试分配一个
			allocConsole.Call()
		} else {
			// 如果已有控制台，确保显示出来
			showWindowProc.Call(hwnd, swShow)
		}
	} else {
		// 如果不要求显示，则尝试隐藏现有的
		hideConsole()
	}

	runTray(srv, cfg, shutdown)
	return nil
}
