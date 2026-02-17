package main

import (
	"fmt"
	"log"
	"syscall"

	"github.com/getlantern/systray"
	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/server"
	"github.com/skratchdot/open-golang/open"
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

func platformRun(srv *server.Server, cfg *config.Config, showConsole bool) error {
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

	// 运行系统托盘
	systray.Run(func() {
		systray.SetIcon(iconData)
		systray.SetTitle("PrismCat")
		systray.SetTooltip("PrismCat LLM Proxy " + config.Version)

		titleOpen, titleQuit := getTrayLabels()
		mOpen := systray.AddMenuItem(titleOpen, "")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(titleQuit, "")

		// 托盘菜单事件循环
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					open.Run(fmt.Sprintf("http://localhost:%d", cfg.Server.Port))
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()

		// 在后台启动服务器
		go func() {
			if err := srv.Start(); err != nil {
				log.Printf("服务器错误: %v", err)
				systray.Quit()
			}
		}()

	}, func() {
		log.Printf("PrismCat %s 正在退出...", config.Version)
	})

	return nil
}
