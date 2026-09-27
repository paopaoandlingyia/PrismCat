//go:build windows || (darwin && cgo)

package main

import (
	"fmt"
	"log"

	"github.com/getlantern/systray"
	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/server"
	"github.com/skratchdot/open-golang/open"
)

// runTray 在系统托盘（Windows）/ 菜单栏（macOS）中运行，并在后台启动服务器。
// 阻塞直到用户选择退出。
//
// macOS 上 systray 通过 [NSApp terminate:] 退出，onExit 返回后进程立即结束，
// main 里的 defer 不会执行，所以收尾工作必须经由 shutdown 在 onExit 里完成。
func runTray(srv *server.Server, cfg *config.Config, shutdown func()) {
	systray.Run(func() {
		setTrayIcon()
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
		shutdown()
	})
}
