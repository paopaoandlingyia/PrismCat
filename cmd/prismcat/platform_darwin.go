//go:build darwin && cgo

package main

import (
	_ "embed"
	"os"
	"os/exec"
	"strings"

	"github.com/getlantern/systray"
	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/server"
)

// 菜单栏模板图标（黑色 + 透明，系统按深浅色自动着色），
// 由 go run ./scripts/macos/gen-tray-icon 生成。
//
//go:embed tray_icon_darwin.png
var trayIconTemplate []byte

func setTrayIcon() {
	systray.SetTemplateIcon(trayIconTemplate, trayIconTemplate)
}

func getTrayLabels() (openTitle, quitTitle string) {
	if preferredLanguageIsChinese() {
		return "打开仪表盘", "退出 PrismCat"
	}
	return "Open Dashboard", "Quit PrismCat"
}

// preferredLanguageIsChinese 读取系统首选语言列表的第一项。
// 从访达启动的 .app 没有 LANG 环境变量，所以要问 defaults。
func preferredLanguageIsChinese() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return strings.HasPrefix(os.Getenv("LANG"), "zh")
	}
	// 输出形如 (\n    "zh-Hans-CN",\n    en\n)
	for _, line := range strings.Split(string(out), "\n") {
		lang := strings.Trim(strings.TrimSpace(line), `"(),`)
		if lang != "" {
			return strings.HasPrefix(lang, "zh")
		}
	}
	return false
}

func platformRun(srv *server.Server, cfg *config.Config, _ bool, shutdown func()) error {
	runTray(srv, cfg, shutdown)
	return nil
}
